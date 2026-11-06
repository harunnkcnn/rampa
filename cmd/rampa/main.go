// Command rampa runs the open-source edition of the Rampa stock panel.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/harunnkcnn/rampa/app"
)

func main() {
	if err := app.Main(context.Background(), os.Args[1:], app.Options{}); err != nil {
		fmt.Fprintln(os.Stderr, "rampa:", err)
		os.Exit(1)
	}
}
