package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/websocket/v2"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"

	"eapteka/ent"
	"eapteka/filesystem"
	"eapteka/migrations"
	"eapteka/pics"
	"eapteka/ui"
)

func timeStrToGoTime(s string) (time.Time, error) {
	ps := strings.Split(s, ":")
	if len(ps) != 3 {
		return time.Time{}, fmt.Errorf("invalid time format")
	}

	h, err := strconv.Atoi(ps[0])
	if err != nil {
		return time.Time{}, fmt.Errorf("parse hour: %w", err)
	}

	m, err := strconv.Atoi(ps[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("parse minute: %w", err)
	}

	tz, err := time.LoadLocation(ps[2])
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time zone: %w", err)
	}

	return time.Date(0, 0, 0, h, m, 0, 0, tz), nil
}

func goTimeToTimeStr(t time.Time) string {
	return t.Format("15:04:") + t.Location().String()
}

func processRecommends(db *sqlx.DB) []int64 {

	rows, err := db.Query(`
		select pp.product_id as product_id, p.created_at as created_at
		from purchase as p
		left join purchase_product as pp on p.id = pp.purchase_id
		where product_id is not null and created_at >= $1
		order by created_at desc ;
	`, time.Now().AddDate(0, -6, 0))
	if err != nil {
		logrus.WithError(err).Error("failed to get products")
		return nil
	}

	purchases := map[int64][]time.Time{}

	for rows.Next() {
		var (
			createdAt time.Time
			productID int64
		)

		err := rows.Scan(&createdAt, &productID)
		if err != nil {
			logrus.WithError(err).Error("failed to get product")
			continue
		}

		purchases[productID] = append(purchases[productID], createdAt)
	}

	var recommends []int64

	const (
		minInterval = 25 * 24 * time.Hour
		maxInterval = 35 * 24 * time.Hour

		maxSubInterval = 1 * 24 * time.Hour
	)

	for pID, ts := range purchases {
		if len(ts) < 2 {
			continue
		}
		var count int
		for i := 1; i < len(ts); i++ {
			interval := ts[i].Sub(ts[i-1])
			if (interval < minInterval || interval > maxInterval) &&
				maxSubInterval > interval {
				break
			}
			count++
		}
		if count > 2 {
			recommends = append(recommends, pID)
		}
	}

	return recommends
}

func main() {
	pgDSN := os.Getenv("POSTGRES_DSN")
	bindAddr := os.Getenv("BIND_ADDR")
	tlsCert := os.Getenv("TLS_CERT")
	tlsKey := os.Getenv("TLS_KEY")

	db, err := sqlx.Open("postgres", pgDSN)
	if err != nil {
		logrus.WithError(err).Fatal("failed to open DB")
	}

	err = migrations.Migrate(pgDSN)
	if err != nil {
		logrus.WithError(err).Fatal("failed to migrate")
	}

	ws := fiber.New()

	ws.Use(recover.New(), logger.New(), cors.New())

	api := ws.Group("/api")

	api.Get("/query", func(ctx *fiber.Ctx) error {
		keyword := ctx.Query("k", "")
		if len(keyword) <= 3 {
			return fiber.NewError(http.StatusBadRequest, "too short keyword")
		}

		var (
			wg    sync.WaitGroup
			ps    []ent.Product
			psErr error
			ss    []ent.Substance
			ssErr error
		)

		wg.Add(1)
		go func() {
			defer wg.Done()
			psErr = db.Select(&ps, `
				select substance_id, p.id as id, p.name as name, description,
				       price, image_id, sku, s.name as substance_name 
				from product p
				    left join substance s on p.substance_id = s.id
				where p.name % $1
				order by similarity(p.name, $1) desc
			`, keyword)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			ssErr = db.Select(&ss, `
				select * from substance s
				where s.name % $1
				order by similarity(name, $1) desc
			`, keyword)
		}()

		wg.Wait()

		if psErr != nil {
			return psErr
		}
		if ssErr != nil {
			return ssErr
		}

		return ctx.JSON(fiber.Map{
			"products":   ps,
			"substances": ss,
		})
	})

	api.Get("/products/:product_id", func(ctx *fiber.Ctx) error {
		pID, err := ctx.ParamsInt("product_id")
		if err != nil {
			return fiber.NewError(http.StatusBadRequest, err.Error())
		}

		var p ent.Product

		err = db.QueryRowx(`
			select substance_id, p.id as id, p.name as name, description,
				   price, image_id, sku, s.name as substance_name 
			from product p
				left join substance s on p.substance_id = s.id
			where p.id = $1
		`, pID).StructScan(&p)
		if err != nil {
			return err
		}

		return ctx.JSON(p)
	})

	api.Get("/products", func(ctx *fiber.Ctx) error {
		var (
			ps          []ent.Product
			substanceID int64
			err         error
		)

		substanceIDstr := ctx.Query("substance_id", "")

		if len(substanceIDstr) != 0 {
			substanceID, err = strconv.ParseInt(substanceIDstr, 10, 64)
			if err != nil {
				return fiber.NewError(http.StatusBadRequest, err.Error())
			}
		}

		if substanceID != 0 {
			err = db.Select(&ps, `
				select p.id as id, p.name as name, description, price, image_id,
						sku, s.name as substance_name, s.id as substance_id
				from product p
					left join substance s on p.substance_id = s.id
				where substance_id = $1
				order by id desc
			`, substanceID)
		} else {
			err = db.Select(&ps, `
				select p.id as id, p.name as name, description, price, image_id,
						sku, s.name as substance_name, s.id as substance_id
				from product p
					left join substance s on p.substance_id = s.id
				order by id desc
			`)
		}
		if err != nil {
			return err
		}

		return ctx.JSON(ps)
	})

	api.Get("/substances", func(ctx *fiber.Ctx) error {
		var (
			ss        []ent.Substance
			productID int64
			err       error
		)

		productIDstr := ctx.Query("product_id", "")

		if len(productIDstr) != 0 {
			productID, err = strconv.ParseInt(productIDstr, 10, 64)
			if err != nil {
				return fiber.NewError(http.StatusBadRequest, err.Error())
			}
		}

		if productID != 0 {
			err = db.Select(&ss, `
				select s.id as id, name 
				from substance s
					left join product p on p.substance_id = s.id
				where p.id = $1
				order by id desc
			`, productID)

		} else {
			err = db.Select(&ss, `
				select id, name
				from substance s
				order by id desc
			`)
		}
		if err != nil {
			return err
		}

		return ctx.JSON(ss)
	})

	api.Get("/purchases", func(ctx *fiber.Ctx) error {
		var ps []ent.Purchase

		err := db.Select(&ps, `select * from purchase order by created_at desc`)
		if err != nil {
			return err
		}

		return ctx.JSON(ps)
	})

	api.Get("/purchase_products", func(ctx *fiber.Ctx) error {
		purchaseIDstr := ctx.Query("purchase_id", "")
		purchaseID, err := strconv.ParseInt(purchaseIDstr, 10, 64)
		if err != nil {
			return fiber.NewError(http.StatusBadRequest, err.Error())
		}

		var ps []ent.Product

		err = db.Select(&ps, `
			select p.id as id, substance_id, p.name as name, description, image_id,
					s.name as substance_name, count, pp.price as purchase_price 
			from purchase_product pp
			    left join product p on pp.product_id = p.id
			    left join substance s on s.id = p.substance_id
			where purchase_id = $1`, purchaseID)
		if err != nil {
			return err
		}

		return ctx.JSON(ps)
	})

	api.Post("/purchases", func(ctx *fiber.Ctx) (err error) {
		var pps []ent.PurchaseProduct

		err = json.Unmarshal(ctx.Body(), &pps)
		if err != nil {
			return fiber.NewError(http.StatusBadRequest, err.Error())
		}

		tx, err := db.BeginTxx(context.TODO(), nil)
		if err != nil {
			return err
		}

		defer func() {
			if err != nil {
				tx.Rollback()
			}
		}()

		var p ent.Purchase

		err = tx.QueryRowx(`insert into purchase default values returning *`).
			StructScan(&p)
		if err != nil {
			return err
		}

		var pIDs []int64
		for _, pp := range pps {
			_, err = tx.Exec(`
				insert into purchase_product(purchase_id, product_id, count, price)
 				values ($1, $2, $3, $4)
			`, p.ID, pp.ProductID, pp.Count, pp.Price)
			if err != nil {
				return err
			}
			pIDs = append(pIDs, pp.ProductID)
		}

		err = tx.Commit()
		if err != nil {
			return err
		}

		err = db.Select(&p.Products, `
			select * from product where id = ANY($1::BIGINT[])
			order by id asc
		`, pq.Array(pIDs))
		if err != nil {
			return err
		}

		return ctx.JSON(p)
	})

	var (
		nsMap = map[int64]ent.Notifier{}
		nsMx  sync.RWMutex
	)
	func() {
		var ns []ent.Notifier
		err = db.Select(&ns, `
			select n.id as id, product_id, schedule, p.name as product_name
				from notifier as n left join product p on p.id = n.product_id
		`)
		if err != nil {
			logrus.WithError(err).Fatal("failed to load notifiers")
		}
		for _, n := range ns {
			nsMap[n.ID] = n
		}
	}()

	api.Get("/notifiers", func(ctx *fiber.Ctx) error {

		nsMx.RLock()
		defer nsMx.RUnlock()

		var ns []ent.Notifier
		for _, n := range nsMap {
			ns = append(ns, n)
		}

		return ctx.JSON(ns)
	})

	api.Post("/notifiers", func(ctx *fiber.Ctx) error {
		var n ent.Notifier

		err = json.Unmarshal(ctx.Body(), &n)
		if err != nil {
			return fiber.NewError(http.StatusBadRequest, err.Error())
		}

		err = db.QueryRowx(`
			select name from product where id = $1
		`, n.ProductID).Scan(&n.ProductName)
		if err != nil {
			return err
		}

		for i, s := range n.Schedule {
			t, err := timeStrToGoTime(s)
			if err != nil {
				return fiber.NewError(http.StatusBadRequest, err.Error())
			}
			n.Schedule[i] = goTimeToTimeStr(t)
		}

		err = db.QueryRowx(`
			insert into notifier(product_id, schedule) values ($1, $2)
			returning id
		`, n.ProductID, pq.Array(n.Schedule)).Scan(&n.ID)
		if err != nil {
			return err
		}

		nsMx.Lock()
		nsMap[n.ID] = n
		nsMx.Unlock()

		return ctx.JSON(n)
	})

	api.Delete("/notifiers/:id", func(ctx *fiber.Ctx) error {
		nID, err := ctx.ParamsInt("id")
		if err != nil {
			return fiber.NewError(http.StatusBadRequest, err.Error())
		}

		_, err = db.Exec(`delete from notifier where id = $1`, nID)
		if err != nil {
			return err
		}

		nsMx.Lock()
		delete(nsMap, int64(nID))
		nsMx.Unlock()

		return ctx.SendStatus(http.StatusOK)
	})

	api.Get("/experts/:substance_id", func(ctx *fiber.Ctx) error {
		sID, err := ctx.ParamsInt("substance_id")
		if err != nil {
			return fiber.NewError(http.StatusBadRequest, err.Error())
		}

		var e ent.Expert

		err = db.QueryRowx(`
			select * from expert where substance_id = $1
		`, sID).StructScan(&e)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ctx.SendStatus(http.StatusNotFound)
			}
			return err
		}

		return ctx.JSON(e)
	})

	var (
		wsNotifierClose = make(chan struct{})
		wsNotifierWg    sync.WaitGroup
	)

	ws.Get("/ws/notifier", websocket.New(func(c *websocket.Conn) {
		wsNotifierWg.Add(1)
		defer wsNotifierWg.Done()
		defer c.Close()

		t := time.NewTicker(30 * time.Second)
		defer t.Stop()

		for {
			select {
			case <-wsNotifierClose:
				return
			case <-t.C:
			}

			now := time.Now()
			nowStr := goTimeToTimeStr(now)

			nsMx.RLock()
			for _, n := range nsMap {
				for _, s := range n.Schedule {
					if nowStr != s {
						continue
					}
					msg := fmt.Sprintf("Вам необходимо выпить лекарство \"%s\".", n.ProductName)
					if err = c.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
						return
					}
				}
			}
			nsMx.RUnlock()
		}
	}))

	var (
		wsRecommendsClose = make(chan struct{})
		wsRecommends      = make(chan ent.Product)
		wsRecommendsWg    sync.WaitGroup
	)

	wsRecommendsWg.Add(1)
	go func() {
		defer wsRecommendsWg.Done()

		t := time.NewTicker(1 * time.Hour)
		var p ent.Product

		for {
			select {
			case <-wsRecommendsClose:
				return
			case <-t.C:
			}

			recommends := processRecommends(db)

			pID := recommends[rand.Intn(len(recommends))]

			err = db.QueryRowx(`
				select p.id as id, p.name as name, description, price, image_id,
						sku, s.name as substance_name, s.id as substance_id
				from product p
					left join substance s on p.substance_id = s.id
				where p.id = $1
			`, pID).StructScan(&p)
			if err != nil {
				logrus.WithError(err).Error("failed to get product")
				continue
			}

			wsRecommends <- p
		}
	}()

	ws.Get("/ws/recommends", websocket.New(func(c *websocket.Conn) {
		wsRecommendsWg.Add(1)
		defer wsRecommendsWg.Done()
		defer c.Close()

		var p ent.Product

		for {
			select {
			case <-wsRecommendsClose:
				return
			case p = <-wsRecommends:
			}

			if err = c.WriteJSON(p); err != nil {
				return
			}
		}
	}))

	ws.Use("/pics", filesystem.New(filesystem.Config{
		Root: http.FS(pics.FS),
	}))

	ws.Use(filesystem.New(filesystem.Config{
		Next: func(c *fiber.Ctx) bool {
			path := string(c.Request().URI().Path())
			return strings.HasPrefix(path, "/api/") ||
				strings.HasPrefix(path, "/pics/") ||
				strings.HasPrefix(path, "/ws/")
		},
		Root:         http.FS(ui.FS),
		Index:        "index.html",
		NotFoundFile: "index.html",
		RootPath:     "dist",
	}))

	var wg sync.WaitGroup

	if tlsCert != "" && tlsKey != "" {
		go func() {
			defer wg.Done()
			err := ws.ListenTLS(bindAddr, tlsCert, tlsKey)
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				logrus.WithError(err).Fatal("failed to start web server")
			}
		}()
	} else {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := ws.Listen(bindAddr)
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				logrus.WithError(err).Fatal("failed to start web server")
			}
		}()
	}

	exit := make(chan os.Signal)
	signal.Notify(exit, os.Interrupt, syscall.SIGTERM)
	<-exit

	close(wsNotifierClose)
	close(wsRecommendsClose)

	err = ws.Shutdown()
	if err != nil {
		logrus.WithError(err).Fatal("failed to shutdown web server")
	}

	wsNotifierWg.Wait()
	wsRecommendsWg.Wait()
	wg.Wait()
}
