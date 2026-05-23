package observer

import (
	"sync"

	instance_model "github.com/EvolutionAPI/evolution-go/pkg/instance/model"
)

type Observer interface {
	HandleEvolutionEvent(instance *instance_model.Instance, eventType string, queueName string, payload []byte)
}

var (
	mu        sync.RWMutex
	observers []Observer
)

func Register(observer Observer) {
	if observer == nil {
		return
	}

	mu.Lock()
	defer mu.Unlock()
	observers = append(observers, observer)
}

func Dispatch(instance *instance_model.Instance, eventType string, queueName string, payload []byte) {
	mu.RLock()
	current := append([]Observer(nil), observers...)
	mu.RUnlock()

	if len(current) == 0 {
		return
	}

	for _, observer := range current {
		payloadCopy := append([]byte(nil), payload...)
		go func(o Observer) {
			defer func() {
				_ = recover()
			}()
			o.HandleEvolutionEvent(instance, eventType, queueName, payloadCopy)
		}(observer)
	}
}
