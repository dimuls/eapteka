package main

import (
	"context"
	"eapteka/migrations"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/jmoiron/sqlx"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"

	"eapteka/ent"
)

func main() {
	pgDSN := os.Getenv("POSTGRES_DSN")
	bindAddr := os.Getenv("BIND_ADDR")

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
		query := ctx.Query("k", "")
		if len(query) < 3 {
			return fiber.NewError(http.StatusBadRequest, "too short query")
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
				select p.*, s.name from product p
				    left join substance s on p.substance_id = s.id
				where to_tsvector(p.name) @@ to_tsquery($1)
			`, query)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			ssErr = db.Select(&ss, `
				select * from substance s
				    left join product p on s.id = p.substance_id 
				where to_tsvector(s.name) @@ to_tsquery($1)
			`, query)
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

	api.Get("/purchases", func(ctx *fiber.Ctx) error {
		var ps []ent.Purchase

		err := db.Select(&ps, `select * from purchase order by created_at desc`)
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
				insert into purchase_product(purchase_id, product_id, count)
 				values ($1, $2, $3)
			`, p.ID, pp.ProductID, pp.Count)
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

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := ws.Listen(bindAddr)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logrus.WithError(err).Fatal("failed to start web server")
		}
	}()

	exit := make(chan os.Signal)
	signal.Notify(exit, os.Interrupt, syscall.SIGTERM)
	<-exit

	err = ws.Shutdown()
	if err != nil {
		logrus.WithError(err).Fatal("failed to shutdown web server")
	}

	wg.Wait()
}
