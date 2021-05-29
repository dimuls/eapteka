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
	Price      int32 `json:"price" db:"price"`
}

type Product struct {
	ID          int64  `json:"id" db:"id"`
	SubstanceID int64  `json:"substance_id" db:"substance_id"`
	Name        string `json:"name" db:"name"`
	Description string `json:"description" db:"description"`
	Price       int32  `json:"price" db:"price"`
	ImageID     int32  `json:"image_id" db:"image_id"`
	SKU         int32  `json:"sku" db:"sku"`

	SubstanceName *string `json:"substance_name" db:"substance_name"`
	Count         int32   `json:"count,omitempty" db:"count"`
	PurchasePrice int32   `json:"purchase_price" db:"purchase_price"`
}

type Substance struct {
	ID   int64  `json:"id" db:"id"`
	Name string `json:"name" db:"name"`

	Products []Product `json:"products,omitempty" db:"-"`
}

type Notifier struct {
	ID        int64    `json:"id" db:"id"`
	ProductID int64    `json:"product_id" db:"product_id"`
	Schedule  []string `json:"schedule" db:"schedule"`

	ProductName string `json:"product_name" db:"product_name"`
}
