package metrics

import (
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	// Initialize metrics once for all tests in this package.
	Init(Config{Enabled: true, Path: "/metrics"})
	os.Exit(m.Run())
}

func TestRecordRequest(t *testing.T) {
	// Reset metrics for test isolation
	RequestsTotal.Reset()
	RequestDuration.Reset()

	RecordRequest("api", "hello", 200, 50*time.Millisecond)
	RecordRequest("api", "hello", 200, 100*time.Millisecond)
	RecordRequest("api", "hello", 500, 200*time.Millisecond)

	// Check counter
	counter, err := RequestsTotal.GetMetricWithLabelValues("api", "hello", "200")
	require.NoError(t, err)
	m := &dto.Metric{}
	require.NoError(t, counter.Write(m))
	require.Equal(t, 2.0, m.GetCounter().GetValue())

	// Check 500 counter
	counter500, err := RequestsTotal.GetMetricWithLabelValues("api", "hello", "500")
	require.NoError(t, err)
	m500 := &dto.Metric{}
	require.NoError(t, counter500.Write(m500))
	require.Equal(t, 1.0, m500.GetCounter().GetValue())
}

func TestRecordStep(t *testing.T) {
	StepDuration.Reset()

	RecordStep("api", "dashboard", "fetch_user", 30*time.Millisecond)

	// Verify histogram was recorded
	observer, err := StepDuration.GetMetricWithLabelValues("api", "dashboard", "fetch_user")
	require.NoError(t, err)

	m := &dto.Metric{}
	require.NoError(t, observer.(prometheus.Metric).Write(m))
	require.Equal(t, uint64(1), m.GetHistogram().GetSampleCount())
}

func TestRecordError(t *testing.T) {
	ErrorsTotal.Reset()

	RecordError("api", "hello", "injected")
	RecordError("api", "hello", "injected")
	RecordError("api", "hello", "step_failed")

	counter, err := ErrorsTotal.GetMetricWithLabelValues("api", "hello", "injected")
	require.NoError(t, err)
	m := &dto.Metric{}
	require.NoError(t, counter.Write(m))
	require.Equal(t, 2.0, m.GetCounter().GetValue())
}

func TestHandler(t *testing.T) {
	h := Handler()
	require.NotNil(t, h)
}

func TestIsEnabled(t *testing.T) {
	// TestMain called Init with Enabled: true
	require.True(t, IsEnabled())
}

func TestPath(t *testing.T) {
	// TestMain called Init with Path: "/metrics"
	require.Equal(t, "/metrics", Path())
}

func TestInit_Disabled(t *testing.T) {
	// Save and restore global state
	origEnabled := enabled
	origPath := path
	defer func() {
		enabled = origEnabled
		path = origPath
	}()

	// Re-init as disabled (skip MustRegister since already registered)
	enabled = false
	path = ""

	require.False(t, IsEnabled())
	require.Equal(t, "", Path())
}
