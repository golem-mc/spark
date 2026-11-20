// Command server is the golem server with spark built in.
package main

import (
	"github.com/golem-mc/golem/app"
	_ "github.com/golem-mc/spark"
)

func main() { app.Main() }
