package service

import (
	"bufio"
	"errors"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

func TestRedisWorksheetReplyBounds(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		value       any
		err         error
		completed   bool
	}{
		{name: "missing", reply: ":0\r\n$0\r\n\r\n", completed: true},
		{name: "empty", reply: ":1\r\n$0\r\n\r\n", value: "", completed: true},
		{name: "overflow probe", reply: ":1\r\n$1025\r\n" + strings.Repeat("x", 1025) + "\r\n", err: ErrQueryResultTooLarge, completed: true},
		{name: "unbounded declared size", reply: ":1\r\n$999999999\r\n", err: ErrQueryBackendFailure},
		{name: "missing key with a value", reply: ":0\r\n$1\r\nx\r\n", err: ErrQueryBackendFailure, completed: true},
		{name: "wrong type", reply: ":1\r\n-WRONGTYPE unexpected type\r\n", err: ErrQueryValidationFailed, completed: true},
		{name: "refused in EXEC", reply: ":1\r\n-NOPERM protected key\r\n", err: ErrQueryNotAllowed, completed: true},
		{name: "invalid UTF-8", reply: ":1\r\n$1\r\n\xff\r\n", err: ErrQueryValidationFailed, completed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := redisWorksheetWire{reader: bufio.NewReader(strings.NewReader(tc.reply))}
			remote := model.QueryExecutionRemoteUnknown
			got, err := wire.getReply(&remote)
			if got != tc.value || !errors.Is(err, tc.err) || (remote == model.QueryExecutionRemoteCompleted) != tc.completed {
				t.Fatalf("value=%v error=%v remote=%s", got, err, remote)
			}
		})
	}
}
