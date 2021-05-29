package main

import (
	"context"
	"eapteka/pics"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

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
				select substance_id, p.name as name, description, price, image_id,
				        s.name as substance_name 
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

		if len(substanceIDstr) == 0 {
			substanceID, err = strconv.ParseInt(substanceIDstr, 10, 64)
			if err != nil {
				return fiber.NewError(http.StatusBadRequest, err.Error())
			}
		}

		if substanceID != 0 {
			err = db.Select(&ps, `
				select substance_id, p.name as name, description, price, image_id,
						s.name as substance_name 
				from product p
					left join substance s on p.substance_id = s.id
				where substance_id = $1
				order by id desc
			`, substanceID)
		} else {
			err = db.Select(&ps, `
				select substance_id, p.name as name, description, price, image_id,
						s.name as substance_name 
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
			ps        []ent.Product
			productID int64
			err       error
		)

		productIDstr := ctx.Query("product_id", "")

		if len(productIDstr) == 0 {
			productID, err = strconv.ParseInt(productIDstr, 10, 64)
			if err != nil {
				return fiber.NewError(http.StatusBadRequest, err.Error())
			}
		}

		if productID != 0 {
			err = db.Select(&ps, `
				select id, name 
				from substance s
					left join product p on p.substance_id = s.id
				where p.id = $1
				order by id desc
			`, productID)

		} else {
			err = db.Select(&ps, `
				select id, name
				from substance s
				order by id desc
			`)
		}
		if err != nil {
			return err
		}

		return ctx.JSON(ps)
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
				        s.name as substance_name, count
			from purchase_product
			    left join product p on purchase_product.product_id = p.id
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

	ws.Use("/pics", filesystem.New(filesystem.Config{
		Next: func(c *fiber.Ctx) bool {
			path := string(c.Request().URI().Path())
			return strings.HasPrefix(path, "/api/")
		},
		Root: http.FS(pics.FS),
	}))

	ws.Use(filesystem.New(filesystem.Config{
		Next: func(c *fiber.Ctx) bool {
			path := string(c.Request().URI().Path())
			return strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/pics/")
		},
		Root:         http.FS(ui.FS),
		Index:        "index.html",
		NotFoundFile: "index.html",
		RootPath:     "dist",
	}))

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
