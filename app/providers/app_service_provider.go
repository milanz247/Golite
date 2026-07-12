package providers

import (
	"fmt"
	"golite/container"
)

type AppServiceProvider struct{}

func (p *AppServiceProvider) Register(c *container.Container) {
	// උදාහරණයක් ලෙස Database service එකක් Bind කිරීම
	c.Bind("db_connection_string", "mysql://root:secret@127.0.0.1:3306/golite_db")
}

func (p *AppServiceProvider) Boot(c *container.Container) {
	// Database connection එක Boot වන අවස්ථාවේදී පරීක්ෂා කිරීම
	db := c.Make("db_connection_string").(string)
	fmt.Printf("[Service Provider] Database Connection Booted: %s\n", db)
}