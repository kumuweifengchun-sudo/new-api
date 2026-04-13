package model

import (
	"math/rand"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

func isSQLiteBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked") ||
		strings.Contains(msg, "database schema is locked") ||
		strings.Contains(msg, "database is busy")
}

func sqliteBusyRetry(opName string, fn func() error) error {
	if !common.UsingSQLite {
		return fn()
	}

	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = fn()
		if !isSQLiteBusyError(err) {
			return err
		}
		backoff := time.Duration(25*(attempt+1)+rand.Intn(40)) * time.Millisecond
		common.SysLog(opName + " hit sqlite busy lock, retrying in " + backoff.String())
		time.Sleep(backoff)
	}
	return err
}
