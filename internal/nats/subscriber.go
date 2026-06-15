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
)

type Subscriber struct {
	db     *sql.DB
	mu     *sync.Mutex
	conn   *natsio.Conn
	msgCnt int64
	histCnt int64
}

func NewSubscriber(database *sql.DB) *Subscriber {
	return &Subscriber{
		db: database,
		mu: &sync.Mutex{},
	}
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

	orderCh := make(chan *natsio.Msg, 256)
	histCh := make(chan *natsio.Msg, 256)

	_, natsErr = s.conn.ChanSubscribe(topicOrders, orderCh)
	if natsErr != nil {
		return natsErr
	}

	_, natsErr = s.conn.ChanSubscribe(topicHistories, histCh)
	if natsErr != nil {
		return natsErr
	}

	log.Printf("NATS connected, subscribed to '%s', '%s'", topicOrders, topicHistories)

	go s.processOrders(orderCh)
	go s.processHistories(histCh)

	return nil
}

func (s *Subscriber) Stop() {
	if s.conn != nil {
		s.conn.Drain()
		s.conn.Close()
	}
}

func (s *Subscriber) processOrders(ch chan *natsio.Msg) {
	for msg := range ch {
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

		s.mu.Lock()
		locID := strconv.Itoa(order.LocationID)
		city := db.LookupCity(s.db, locID)
		if err := db.InsertMarketOrder(s.db, city, order.ItemID, order.QualityLevel, order.Price, order.Amount, order.AuctionType); err != nil {
			log.Printf("failed to insert market order: %v", err)
		}
		s.mu.Unlock()
	}
}

func (s *Subscriber) processHistories(ch chan *natsio.Msg) {
	for msg := range ch {
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

		s.mu.Lock()
		locName := db.LookupCity(s.db, strconv.Itoa(upload.LocationId))
		log.Printf("history city lookup: id=%d name=%s", upload.LocationId, locName)
		if err := db.InsertMarketHistory(s.db, upload.AlbionIdString, locName, int(upload.QualityLevel), int(upload.Timescale), upload.Histories); err != nil {
			log.Printf("failed to insert market history: %v", err)
		} else {
			log.Printf("history insert ok: item=%s loc=%s ql=%d ts=%d pts=%d",
				upload.AlbionIdString, locName, upload.QualityLevel, upload.Timescale, len(upload.Histories))
		}
		s.mu.Unlock()
	}
}
