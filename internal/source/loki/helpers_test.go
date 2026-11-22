package loki

import (
	"encoding/json"
	"reflect"

	"github.com/sperano/sillage/internal/model"
)

func eventsEqual(a, b model.UsageEvent) bool { return reflect.DeepEqual(a, b) }

func dump(e model.UsageEvent) string {
	b, _ := json.Marshal(e)
	return string(b)
}
