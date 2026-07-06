package nats

import (
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	natsio "github.com/nats-io/nats.go"

	"albion-helper/internal/db"
	"albion-helper/internal/models"
)

const (
	serverURL      = "nats://public:thenewalbiondata@nats.albion-online-data.com:34222"
	topicOrders    = "marketorders.deduped"
	topicHistories = "markethistories.deduped"

	orderBatchSize    = 50
	orderFlushInterval = 100 * time.Millisecond
)

type orderEntry struct {
	city        string
	itemID      string
	quality     int
	price       int
	amount      int
	auctionType string
}

type Subscriber struct {
	db      *sql.DB
	conn    *natsio.Conn
	subOrd  *natsio.Subscription
	subHist *natsio.Subscription
	msgCnt  int64
	histCnt int64
	done    chan struct{}

	orderMu   sync.Mutex
	orders    []orderEntry
	orderCond *sync.Cond
}

func NewSubscriber(database *sql.DB) *Subscriber {
	s := &Subscriber{
		db:   database,
		done: make(chan struct{}),
	}
	s.orderCond = sync.NewCond(&s.orderMu)
	return s
}

func (s *Subscriber) Start() error {
	logFile, err := os.OpenFile("nats.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(logFile)
	}

	var natsErr error
	s.conn, natsErr = natsio.Connect(serverURL,
		natsio.RetryOnFailedConnect(true),
		natsio.MaxReconnects(-1),
		natsio.ReconnectWait(2*time.Second),
		natsio.Timeout(10*time.Second),
		natsio.DisconnectErrHandler(func(_ *natsio.Conn, err error) {
			log.Printf("NATS disconnected: %v", err)
		}),
		natsio.ReconnectHandler(func(_ *natsio.Conn) {
			log.Println("NATS reconnected")
		}),
		natsio.ClosedHandler(func(_ *natsio.Conn) {
			log.Println("NATS connection closed")
		}),
	)
	if natsErr != nil {
		return natsErr
	}

	orderCh := make(chan *natsio.Msg, 64)
	histCh := make(chan *natsio.Msg, 64)

	s.subOrd, natsErr = s.conn.ChanSubscribe(topicOrders, orderCh)
	if natsErr != nil {
		return natsErr
	}
	s.subOrd.SetPendingLimits(1000, 4*1024*1024)

	s.subHist, natsErr = s.conn.ChanSubscribe(topicHistories, histCh)
	if natsErr != nil {
		return natsErr
	}
	s.subHist.SetPendingLimits(1000, 4*1024*1024)

	log.Printf("NATS connected, subscribed to '%s', '%s'", topicOrders, topicHistories)

	go s.processOrders(orderCh)
	go s.processHistories(histCh)
	go s.flushLoop()

	return nil
}

func (s *Subscriber) Stop() {
	close(s.done)
	s.orderCond.Signal()
	if s.subOrd != nil {
		s.subOrd.Unsubscribe()
	}
	if s.subHist != nil {
		s.subHist.Unsubscribe()
	}
	if s.conn != nil {
		s.conn.Drain()
		s.conn.Close()
	}
}

func (s *Subscriber) processOrders(ch chan *natsio.Msg) {
	for {
		select {
		case <-s.done:
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if msg == nil {
				continue
			}
			cnt := atomic.AddInt64(&s.msgCnt, 1)
			if cnt%100 == 0 {
				log.Printf("processed %d orders", cnt)
			}

			var order models.MarketOrder
			if err := json.Unmarshal(msg.Data, &order); err != nil {
				log.Printf("failed to unmarshal market order: %v", err)
				continue
			}

			locID := strconv.Itoa(order.LocationID)
			city := db.LookupCity(s.db, locID)

			s.orderMu.Lock()
			s.orders = append(s.orders, orderEntry{
				city:        city,
				itemID:      order.ItemID,
				quality:     order.QualityLevel,
				price:       order.Price,
				amount:      order.Amount,
				auctionType: order.AuctionType,
			})
			shouldFlush := len(s.orders) >= orderBatchSize
			s.orderMu.Unlock()

			if shouldFlush {
				s.orderCond.Signal()
			}
		}
	}
}

func (s *Subscriber) flushLoop() {
	for {
		s.orderMu.Lock()
		for len(s.orders) == 0 {
			s.orderCond.Wait()
			select {
			case <-s.done:
				s.orderMu.Unlock()
				s.flushOrders()
				return
			default:
			}
		}
		s.orderMu.Unlock()

		s.flushOrders()

		select {
		case <-s.done:
			s.flushOrders()
			return
		case <-time.After(orderFlushInterval):
		}
	}
}

func (s *Subscriber) flushOrders() {
	s.orderMu.Lock()
	if len(s.orders) == 0 {
		s.orderMu.Unlock()
		return
	}
	batch := s.orders
	s.orders = nil
	s.orderMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		log.Printf("flushOrders: begin tx: %v", err)
		return
	}

	stmt, err := tx.Prepare(`
		INSERT INTO marketorders (item_id, city, quality_level, price, amount, auction_type)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		log.Printf("flushOrders: prepare: %v", err)
		tx.Rollback()
		return
	}

	var failed int
	for _, o := range batch {
		_, err := stmt.Exec(o.itemID, o.city, o.quality, o.price, o.amount, o.auctionType)
		if err != nil {
			failed++
			if failed <= 3 {
				log.Printf("flushOrders: insert: %v", err)
			}
		}
	}
	stmt.Close()

	if err := tx.Commit(); err != nil {
		log.Printf("flushOrders: commit: %v", err)
	}
}

func (s *Subscriber) processHistories(ch chan *natsio.Msg) {
	for {
		select {
		case <-s.done:
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if msg == nil {
				continue
			}
			log.Printf("history msg received (%d bytes)", len(msg.Data))
			cnt := atomic.AddInt64(&s.histCnt, 1)
			if cnt%10 == 0 {
				log.Printf("processed %d history uploads", cnt)
			}

			var upload models.MarketHistoriesUpload
			if err := json.Unmarshal(msg.Data, &upload); err != nil {
				log.Printf("failed to unmarshal market history: %v", err)
				continue
			}
			log.Printf("history unmarshal ok: item=%s loc=%d ql=%d ts=%d pts=%d",
				upload.AlbionIdString, upload.LocationId, upload.QualityLevel, upload.Timescale, len(upload.Histories))

			locName := db.LookupCity(s.db, strconv.Itoa(upload.LocationId))
			log.Printf("history city lookup: id=%d name=%s", upload.LocationId, locName)
			if err := db.InsertMarketHistory(s.db, upload.AlbionIdString, locName, int(upload.QualityLevel), int(upload.Timescale), upload.Histories); err != nil {
				log.Printf("failed to insert market history: %v", err)
			} else {
				log.Printf("history insert ok: item=%s loc=%s ql=%d ts=%d pts=%d",
					upload.AlbionIdString, locName, upload.QualityLevel, upload.Timescale, len(upload.Histories))
			}
		}
	}
}