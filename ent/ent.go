package ent

import "time"

type Purchase struct {
	ID        int64     `json:"id" db:"id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`

	Products []Product `json:"products,omitempty" db:"-"`
}

type PurchaseProduct struct {
	PurchaseID int64 `json:"purchase_id" db:"purchase_id"`
	ProductID  int64 `json:"product_id" db:"product_id"`
	Count      int32 `json:"count" db:"count"`
}

type Product struct {
	ID          int64  `json:"id" db:"id"`
	SubstanceID int64  `json:"substance_id" db:"substance_id"`
	Name        string `json:"name" db:"name"`
	Description string `json:"description" db:"description"`
	Price       int32  `json:"price" db:"price"`
	ImageID     int32  `json:"image_id" db:"image_id"`

	SubstanceName *string   `json:"substance_name" db:"substance_name"`
	Substance     Substance `json:"substances,omitempty" db:"-"`
}

type Substance struct {
	ID   int64  `json:"id" db:"id"`
	Name string `json:"name" db:"name"`

	Products []Product `json:"products,omitempty" db:"-"`
}
