package main

import (
	"encoding/csv"
	"os"
)

func main() {

	df, err := os.Open("data.csv")
	if err != nil {
		panic(err)
	}

	defer df.Close()

	r := csv.NewReader(df)

	r.FieldsPerRecord = 5
	r.LazyQuotes = true

	data, err := r.ReadAll()
	if err != nil {
		panic(err)
	}

	for _, d := range data {

	}
}
