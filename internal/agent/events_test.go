package agent

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventBus_PublishSubscribe(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	bus.Publish(Event{Type: "checkpoint", Data: "test"})

	select {
	case evt := <-ch:
		assert.Equal(t, "checkpoint", evt.Type)
	case <-time.After(time.Second):
		t.Fatal("expected event")
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	bus := NewEventBus()
	ch1 := bus.Subscribe()
	ch2 := bus.Subscribe()
	defer bus.Unsubscribe(ch1)
	defer bus.Unsubscribe(ch2)

	bus.Publish(Event{Type: "op", Data: "x"})

	for _, ch := range []chan Event{ch1, ch2} {
		select {
		case evt := <-ch:
			assert.Equal(t, "op", evt.Type)
		case <-time.After(time.Second):
			t.Fatal("expected event on both channels")
		}
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe()
	bus.Unsubscribe(ch)

	// Publishing after unsubscribe should not panic
	bus.Publish(Event{Type: "test", Data: nil})
}

func TestEventBus_SlowSubscriberDropsEvents(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	// Fill the buffer (32 events)
	for i := 0; i < 50; i++ {
		bus.Publish(Event{Type: "flood", Data: i})
	}

	// Should not block or panic
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	assert.LessOrEqual(t, count, 32)
}

func TestAgentServer_SSEHeaders(t *testing.T) {
	_, ts := setupAgentTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/v1/events", nil)
	require.NoError(t, err)

	type result struct {
		resp *http.Response
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := http.DefaultTransport.RoundTrip(req) //nolint:bodyclose
		done <- result{resp, err}
	}()

	select {
	case res := <-done:
		require.NoError(t, res.err)
		defer res.resp.Body.Close()
		assert.Equal(t, "text/event-stream", res.resp.Header.Get("Content-Type"))
		assert.Equal(t, "no-cache", res.resp.Header.Get("Cache-Control"))
	case <-ctx.Done():
		t.Fatal("SSE request timed out before receiving headers")
	}
}
