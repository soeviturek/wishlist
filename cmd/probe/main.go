// Command probe is a quick CLI tool to test store scrapers.
package main

import (
	"fmt"
	"os"

	"wishlist-tracker/internal/stores"
)

func main() {
	url := "https://www.woolworths.com.au/shop/productdetails/708119/monster-energy-juice-mango-loco"
	if len(os.Args) > 1 {
		url = os.Args[1]
	}

	store, err := stores.Detect(url)
	if err != nil {
		fmt.Println("Detect error:", err)
		os.Exit(1)
	}
	fmt.Printf("Store: %s\n", store.Name())

	product, err := store.GetProduct(url)
	if err != nil {
		fmt.Println("GetProduct error:", err)
		os.Exit(1)
	}

	fmt.Printf("Name:  %s\n", product.Name)
	fmt.Printf("Price: $%.2f\n", product.Price)
	fmt.Printf("Image: %s\n", product.ImageURL)
}
