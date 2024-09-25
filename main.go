package main

import (
	"github.com/ronxjansen/ferry/cmd"
)

func main() {
	// config, err := loadConfig("ferry.yaml")
	// if err != nil {
	// 	logger.Fatal("Failed to load config", zap.Error(err))
	// }

	// manager := NewCommandManager(*config)

	// err = manager.Run(setup)
	// if err != nil {
	// 	logger.Fatal("Failed to run commands", zap.Error(err))
	// }

	cmd.Execute()
}
