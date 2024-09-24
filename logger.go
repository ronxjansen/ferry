package main

import (
	prettyconsole "github.com/thessem/zap-prettyconsole"
	"go.uber.org/zap"
)

var logger = prettyconsole.NewLogger(zap.DebugLevel)
