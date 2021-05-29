package main

import (
	"context"
	"eapteka/pics"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gofiber/websocket/v2"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"

	"eapteka/ent"
	"eapteka/filesystem"
	"eapteka/migrations"
	"eapteka/ui"
)

func timeStrToGoTime(s string) (time.Time, error) {
	ps := strings.SplitN(s, ": ", 3)

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
	return t.Format("04:05 MST")
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
			ss    []ent.Product
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
						sku, s.name as substance_name
				from product p
					left join substance s on p.substance_id = s.id
				where substance_id = $1
				order by id desc
			`, substanceID)
		} else {
			err = db.Select(&ps, `
				select p.id as id, p.name as name, description, price, image_id,
						sku, s.name as substance_name 
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
			select substance_id, p.name as name, description, price, image_id,
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
 				values ($1, $2, $3)
			`, p.ID, pp.ProductID, pp.Count, pp.Price)
			if err != nil {
				return err
			}
			pIDs = append(pIDs, pp.ProductID)
		}

		err = db.Select(&p.Products, `
			select * from product where id = ANY($1::BIGINT[])
			order by id asc
		`, pIDs)
		if err != nil {
			return err
		}

		return ctx.JSON(p)
	})

	var (
		wsClose chan struct{}
		wsWg    sync.WaitGroup
	)

	ws.Get("/ws/notifier", websocket.New(func(c *websocket.Conn) {
		wsWg.Add(1)
		defer wsWg.Done()
		defer c.Close()

		var notifiers []ent.Notifier

		err = db.Select(&notifiers, `
			select n.id as id, product_id, schedule, p.name as product_name
				from notifier as n left join product p on p.id = n.product_id
		`)
		if err != nil {
			return
		}

		nsMap := map[string][]ent.Notifier{}

		for _, n := range notifiers {
			for _, s := range n.Schedule {
				nsMap[s] = append(nsMap[s], n)
			}
		}

		t := time.NewTicker(30 * time.Second)

		for {
			select {
			case <-wsClose:
				return
			case <-t.C:
			}

			now := time.Now()
			nowStr := goTimeToTimeStr(now)

			for s, ns := range nsMap {
				if nowStr != s {
					continue
				}

				for _, n := range ns {
					msg := fmt.Sprintf("Вам необходимо выпить лекарство \"%s\".", n.ProductName)
					if err = c.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
						return
					}
				}
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

	close(wsClose)

	err = ws.Shutdown()
	if err != nil {
		logrus.WithError(err).Fatal("failed to shutdown web server")
	}

	wsWg.Wait()
	wg.Wait()
}
