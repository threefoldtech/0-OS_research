package aggregated

import (
	"sync"
	"time"
)

// AggregationMode type
type AggregationMode int

const (
	// AverageMode aggregation mode always keep track of the average over
	// the configured periods
	AverageMode AggregationMode = iota
	// DifferentialMode aggregation mode always keeps track of the difference from
	// the last value divided by the seconds from last value
	DifferentialMode
)

// Aggregated represents an aggregated value
type Aggregated struct {
	Mode       AggregationMode `json:"mode"`
	Samples    []*Sample       `json:"samples"`
	Durations  []time.Duration `json:"durations"`
	Last       float64         `json:"last"`
	LastUpdate time.Time       `json:"last-update"`

	m sync.RWMutex
}

// NewAggregatedMetric returns an aggregated metric over given period
func NewAggregatedMetric(mode AggregationMode, durations ...time.Duration) Aggregated {
	if len(durations) == 0 {
		panic("at least one duration is needed")
	}

	return Aggregated{Mode: mode, Durations: durations}
}

func (a *Aggregated) sample(t time.Time, value float64) float64 {
	a.m.Lock()
	defer a.m.Unlock()

	if len(a.Samples) == 0 {
		for _, d := range a.Durations {
			a.Samples = append(a.Samples, NewAlignedSample(t, d))
		}
	}

	last := a.Last
	lastUpdate := a.LastUpdate
	a.Last = value
	a.LastUpdate = time.Now()
	if a.Mode == DifferentialMode {
		// probably first update, so we keep track
		// only of last value.
		if last == 0 {
			return 0
		}
		// otherwise the value is the difference (increase)
		seconds := float64(a.LastUpdate.Sub(lastUpdate)) / float64(time.Second)
		value = (value - last) / seconds
	}

	// update all samples
	var avg float64
	for i, s := range a.Samples {
		sampleAvg, err := s.Sample(t, value)
		if err == ErrValueIsAfterPeriod {
			// sample period has passed, so we need to
			// create a new sample.
			// QUESTION: push this sample to history?
			s = NewAlignedSample(t, s.Width())
			s.Sample(t, value)
			a.Samples[i] = s
			avg = value
		}

		// we only return the avg of the first defined sample
		if i == 0 {
			avg = sampleAvg
		}
	}

	return avg
}

// Sample update the aggregated value with given values
func (a *Aggregated) Sample(value float64) float64 {
	return a.sample(time.Now(), value)
}

// CurrentSamples return a copy of the current samples
func (a *Aggregated) CurrentSamples() []Sample {
	a.m.RLock()
	defer a.m.RUnlock()

	v := make([]Sample, len(a.Samples))
	for i, sample := range a.Samples {
		v[i] = *sample
	}

	return v
}

// Averages extract averages from given samples
func Averages(samples []Sample) []float64 {
	values := make([]float64, 0, cap(samples))
	for i := range samples {
		values[i] = samples[i].Average()
	}

	return values
}
