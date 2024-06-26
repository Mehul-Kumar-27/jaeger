// Copyright (c) 2018 The Jaeger Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package adaptive

import (
	"errors"
	"flag"

	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/jaegertracing/jaeger/cmd/collector/app/sampling/strategystore"
	"github.com/jaegertracing/jaeger/pkg/distributedlock"
	"github.com/jaegertracing/jaeger/pkg/metrics"
	"github.com/jaegertracing/jaeger/plugin"
	"github.com/jaegertracing/jaeger/plugin/sampling/leaderelection"
	"github.com/jaegertracing/jaeger/storage"
	"github.com/jaegertracing/jaeger/storage/samplingstore"
)

var _ plugin.Configurable = (*Factory)(nil)

// Factory implements strategystore.Factory for an adaptive strategy store.
type Factory struct {
	options        *Options
	logger         *zap.Logger
	metricsFactory metrics.Factory
	lock           distributedlock.Lock
	store          samplingstore.Store
	participant    *leaderelection.DistributedElectionParticipant
}

// NewFactory creates a new Factory.
func NewFactory() *Factory {
	return &Factory{
		options:        &Options{},
		logger:         zap.NewNop(),
		metricsFactory: metrics.NullFactory,
		lock:           nil,
		store:          nil,
	}
}

// AddFlags implements plugin.Configurable
func (*Factory) AddFlags(flagSet *flag.FlagSet) {
	AddFlags(flagSet)
}

// InitFromViper implements plugin.Configurable
func (f *Factory) InitFromViper(v *viper.Viper, _ *zap.Logger) {
	f.options.InitFromViper(v)
}

// Initialize implements strategystore.Factory
func (f *Factory) Initialize(metricsFactory metrics.Factory, ssFactory storage.SamplingStoreFactory, logger *zap.Logger) error {
	if ssFactory == nil {
		return errors.New("sampling store factory is nil. Please configure a backend that supports adaptive sampling")
	}
	var err error
	f.logger = logger
	f.metricsFactory = metricsFactory
	f.lock, err = ssFactory.CreateLock()
	if err != nil {
		return err
	}
	f.store, err = ssFactory.CreateSamplingStore(f.options.AggregationBuckets)
	if err != nil {
		return err
	}
	f.participant = leaderelection.NewElectionParticipant(f.lock, defaultResourceName, leaderelection.ElectionParticipantOptions{
		FollowerLeaseRefreshInterval: f.options.FollowerLeaseRefreshInterval,
		LeaderLeaseRefreshInterval:   f.options.LeaderLeaseRefreshInterval,
		Logger:                       f.logger,
	})
	f.participant.Start()

	return nil
}

// CreateStrategyStore implements strategystore.Factory
func (f *Factory) CreateStrategyStore() (strategystore.StrategyStore, strategystore.Aggregator, error) {
	s := NewStrategyStore(*f.options, f.logger, f.participant, f.store)
	a, err := NewAggregator(*f.options, f.logger, f.metricsFactory, f.participant, f.store)
	if err != nil {
		return nil, nil, err
	}

	s.Start()
	a.Start()

	return s, a, nil
}

// Closes the factory
func (f *Factory) Close() error {
	return f.participant.Close()
}
