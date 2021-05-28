package main

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"

	_ "github.com/lib/pq"

	"eapteka/data"
)

func exitErr(err error) {
	fmt.Println(err)
	os.Exit(1)
}

func main() {

	df, err := data.FS.Open("data.csv")
	if err != nil {
		panic(err)
	}

	defer df.Close()

	r := csv.NewReader(df)

	r.FieldsPerRecord = 5
	r.LazyQuotes = true

	data, err := r.ReadAll()
	if err != nil {
		exitErr(err)
	}

	db, err := sql.Open("postgres", os.Getenv("POSTGRES_DSN"))
	if err != nil {
		exitErr(err)
	}

	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		exitErr(err)
	}

	ss := map[string]int64{}

	for _, d := range data {
		d[1] = strings.TrimSpace(d[1])
		ss[d[1]] = 0
	}

	for s := range ss {
		var id int64
		err = tx.QueryRow(`
			insert into substance(name) values ($1) returning id
		`, s).Scan(&id)
		if err != nil {
			tx.Rollback()
			exitErr(err)
		}
		ss[s] = id
	}

	for _, d := range data {
		price, err := strconv.Atoi(strings.TrimSpace(d[3]))
		if err != nil {
			tx.Rollback()
			exitErr(err)
		}

		imageID, err := strconv.Atoi(strings.TrimSpace(d[4]))
		if err != nil {
			tx.Rollback()
			exitErr(err)
		}

		_, err = tx.Exec(`
			insert into product(substance_id, name, description, price, image_id)
			values ($1, $2, $3, $4, $5)
		`, ss[d[1]], strings.TrimSpace(d[0]), d[2], price, imageID)
		if err != nil {
			tx.Rollback()
			exitErr(err)
		}
	}

	err = tx.Commit()
	if err != nil {
		exitErr(err)
	}
}
