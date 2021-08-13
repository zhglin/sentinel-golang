// Copyright 1999-2020 Alibaba Group Holding Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package circuitbreaker

import (
	"reflect"
	"sync/atomic"

	"github.com/alibaba/sentinel-golang/core/base"
	sbase "github.com/alibaba/sentinel-golang/core/stat/base"
	"github.com/alibaba/sentinel-golang/logging"
	"github.com/alibaba/sentinel-golang/util"
	"github.com/pkg/errors"
)

// State
//  Circuit Breaker State Machine:
//
//                                 switch to open based on rule
//				+-----------------------------------------------------------------------+
//				|                                                                       |
//				|                                                                       v
//		+----------------+                   +----------------+      Probe      +----------------+
//		|                |                   |                |<----------------|                |
//		|                |   Probe succeed   |                |                 |                |
//		|     Closed     |<------------------|    HalfOpen    |                 |      Open      |
//		|                |                   |                |   Probe failed  |                |
//		|                |                   |                +---------------->|                |
//		+----------------+                   +----------------+                 +----------------+
type State int32

// 熔断器的状态
const (
	Closed   State = iota // 关闭
	HalfOpen              // 探测
	Open                  // 熔断
)

// 创建熔断器状态
func newState() *State {
	var state State
	state = Closed

	return &state
}

func (s *State) String() string {
	switch s.get() {
	case Closed:
		return "Closed"
	case HalfOpen:
		return "HalfOpen"
	case Open:
		return "Open"
	default:
		return "Undefined"
	}
}

// 返回当前的状态
func (s *State) get() State {
	return State(atomic.LoadInt32((*int32)(s)))
}

func (s *State) set(update State) {
	atomic.StoreInt32((*int32)(s), int32(update))
}

// 更新状态
func (s *State) cas(expect State, update State) bool {
	return atomic.CompareAndSwapInt32((*int32)(s), int32(expect), int32(update))
}

// StateChangeListener listens on the circuit breaker state change event
// 监听断路器状态改变事件，prev代表切换前的状态，rule表示当前熔断器对应的规则，
type StateChangeListener interface {
	// OnTransformToClosed is triggered when circuit breaker state transformed to Closed.
	// Argument rule is copy from circuit breaker's rule, any changes of rule don't take effect for circuit breaker
	// Copying rule has a performance penalty and avoids invalid listeners as much as possible
	// 当断路器状态转换为闭合时触发。参数规则是对断路器规则的复制，任何规则的更改都不会对断路器生效。复制规则有性能损失，并尽可能避免无效的监听器
	OnTransformToClosed(prev State, rule Rule)

	// OnTransformToOpen is triggered when circuit breaker state transformed to Open.
	// The "snapshot" indicates the triggered value when the transformation occurs.
	// Argument rule is copy from circuit breaker's rule, any changes of rule don't take effect for circuit breaker
	// Copying rule has a performance penalty and avoids invalid listeners as much as possible
	// 熔断器切换到 Open 状态时候会调用改函数，snapshot表示触发熔断的值
	OnTransformToOpen(prev State, rule Rule, snapshot interface{})

	// OnTransformToHalfOpen is triggered when circuit breaker state transformed to HalfOpen.
	// Argument rule is copy from circuit breaker's rule, any changes of rule don't take effect for circuit breaker
	// Copying rule has a performance penalty and avoids invalid listeners as much as possible
	// 熔断器切换到 HalfOpen 状态时候会调用改函数,
	OnTransformToHalfOpen(prev State, rule Rule)
}

// CircuitBreaker is the basic interface of circuit breaker
// 断路器的基本接口
type CircuitBreaker interface {
	// BoundRule returns the associated circuit breaking rule.
	// 返回相关的断路规则。
	BoundRule() *Rule
	// BoundStat returns the associated statistic data structure.
	// 返回关联的统计数据结构。
	BoundStat() interface{}
	// TryPass acquires permission of an invocation only if it is available at the time of invocation.
	// 请求是否被熔断
	TryPass(ctx *base.EntryContext) bool
	// CurrentState returns current state of the circuit breaker.
	CurrentState() State
	// OnRequestComplete record a completed request with the given response time as well as error (if present),
	// and handle state transformation of the circuit breaker.
	// OnRequestComplete is called only when a passed invocation finished.
	OnRequestComplete(rtt uint64, err error)
}

//================================= circuitBreakerBase ====================================
// circuitBreakerBase encompasses the common fields of circuit breaker.
// 熔断器的基本信息
type circuitBreakerBase struct {
	rule *Rule
	// retryTimeoutMs represents recovery timeout (in milliseconds) before the circuit breaker opens.
	// During the open period, no requests are permitted until the timeout has elapsed.
	// After that, the circuit breaker will transform to half-open state for trying a few "trial" requests.
	// retryTimeoutMs表示断路器打开前的恢复超时时间(毫秒)。
	// 在打开期间，在超时结束之前，不允许任何请求。之后，断路器将转变为半开状态，以尝试几个“试验”请求。
	retryTimeoutMs uint32
	// nextRetryTimestampMs is the time circuit breaker could probe
	// nextRetryTimestampMs是断路器可以探测的时间
	nextRetryTimestampMs uint64
	// state is the state machine of circuit breaker
	// 熔断器的状态信息
	state *State
}

func (b *circuitBreakerBase) BoundRule() *Rule {
	return b.rule
}

// CurrentState 获取断路器状态
func (b *circuitBreakerBase) CurrentState() State {
	return b.state.get()
}

// 是否可以进行探测
func (b *circuitBreakerBase) retryTimeoutArrived() bool {
	return util.CurrentTimeMillis() >= atomic.LoadUint64(&b.nextRetryTimestampMs)
}

// 更新可以探测的时间
func (b *circuitBreakerBase) updateNextRetryTimestamp() {
	atomic.StoreUint64(&b.nextRetryTimestampMs, util.CurrentTimeMillis()+uint64(b.retryTimeoutMs))
}

// fromClosedToOpen updates circuit breaker state machine from closed to open.
// Return true only if current goroutine successfully accomplished the transformation.
func (b *circuitBreakerBase) fromClosedToOpen(snapshot interface{}) bool {
	if b.state.cas(Closed, Open) {
		b.updateNextRetryTimestamp()
		for _, listener := range stateChangeListeners {
			listener.OnTransformToOpen(Closed, *b.rule, snapshot)
		}
		return true
	}
	return false
}

// fromOpenToHalfOpen updates circuit breaker state machine from open to half-open.
// Return true only if current goroutine successfully accomplished the transformation.
// 将断路器状态机从开状态更新为半开状态。仅当当前goroutine成功完成转换时返回true。
func (b *circuitBreakerBase) fromOpenToHalfOpen(ctx *base.EntryContext) bool {
	if b.state.cas(Open, HalfOpen) { // 确保只有一个请求进行探测
		// 监听器
		for _, listener := range stateChangeListeners {
			listener.OnTransformToHalfOpen(Open, *b.rule)
		}

		entry := ctx.Entry()
		if entry == nil {
			logging.Error(errors.New("nil entry"), "Nil entry in circuitBreakerBase.fromOpenToHalfOpen()", "rule", b.rule)
		} else {
			// add hook for entry exit
			// if the current circuit breaker performs the probe through this entry, but the entry was blocked,
			// this hook will guarantee current circuit breaker state machine will rollback to Open from Half-Open
			// 如果当前断路器通过该条目执行探测，但该条目被阻止，该钩子将保证当前断路器状态机将从Half-Open回滚到Open
			entry.WhenExit(func(entry *base.SentinelEntry, ctx *base.EntryContext) error {
				if ctx.IsBlocked() && b.state.cas(HalfOpen, Open) { // 探测异常改为open
					for _, listener := range stateChangeListeners {
						listener.OnTransformToOpen(HalfOpen, *b.rule, 1.0)
					}
				}
				return nil
			})
		}
		return true
	}
	return false
}

// fromHalfOpenToOpen updates circuit breaker state machine from half-open to open.
// Return true only if current goroutine successfully accomplished the transformation.
// 将断路器状态机从半开状态更新为打开状态。仅当当前goroutine成功完成转换时返回true。
func (b *circuitBreakerBase) fromHalfOpenToOpen(snapshot interface{}) bool {
	if b.state.cas(HalfOpen, Open) { // 半开更新成打开
		b.updateNextRetryTimestamp()                    // 更新探测时间
		for _, listener := range stateChangeListeners { //调用监听器
			listener.OnTransformToOpen(HalfOpen, *b.rule, snapshot)
		}
		return true
	}
	return false
}

// fromHalfOpenToOpen updates circuit breaker state machine from half-open to closed
// Return true only if current goroutine successfully accomplished the transformation.
// 将断路器状态机从半开状态更新为关闭状态。只有当前程序成功地完成了转换，才返回true。
func (b *circuitBreakerBase) fromHalfOpenToClosed() bool {
	if b.state.cas(HalfOpen, Closed) {
		for _, listener := range stateChangeListeners {
			listener.OnTransformToClosed(HalfOpen, *b.rule)
		}
		return true
	}
	return false
}

//================================= slowRtCircuitBreaker ====================================
// 慢调用比例策略统计信息
type slowRtCircuitBreaker struct {
	circuitBreakerBase
	stat                *slowRequestLeapArray // 统计信息
	maxAllowedRt        uint64                //多长时间的相应记为慢请求
	maxSlowRequestRatio float64               //慢请求的阀值
	minRequestAmount    uint64                // 触发断路的最小请求数
}

// 指定rule，滑动窗口创建slowRtCircuitBreaker
func newSlowRtCircuitBreakerWithStat(r *Rule, stat *slowRequestLeapArray) *slowRtCircuitBreaker {
	return &slowRtCircuitBreaker{
		circuitBreakerBase: circuitBreakerBase{
			rule:                 r,
			retryTimeoutMs:       r.RetryTimeoutMs,
			nextRetryTimestampMs: 0,
			state:                newState(),
		},
		stat:                stat,
		maxAllowedRt:        r.MaxAllowedRtMs,
		maxSlowRequestRatio: r.Threshold,
		minRequestAmount:    r.MinRequestAmount,
	}
}

// 创建slowRtCircuitBreaker
func newSlowRtCircuitBreaker(r *Rule) (*slowRtCircuitBreaker, error) {
	// 创建滑动窗口
	interval := r.StatIntervalMs
	bucketCount := getRuleStatSlidingWindowBucketCount(r)
	stat := &slowRequestLeapArray{}
	leapArray, err := sbase.NewLeapArray(bucketCount, interval, stat)
	if err != nil {
		return nil, err
	}
	stat.data = leapArray

	return newSlowRtCircuitBreakerWithStat(r, stat), nil
}

func (b *slowRtCircuitBreaker) BoundStat() interface{} {
	return b.stat
}

// TryPass checks circuit breaker based on state machine of circuit breaker.
// 基于断路器状态机对断路器进行检测。
func (b *slowRtCircuitBreaker) TryPass(ctx *base.EntryContext) bool {
	curStatus := b.CurrentState()
	if curStatus == Closed { // 关闭状态
		return true
	} else if curStatus == Open {
		// switch state to half-open to probe if retry timeout
		// 切换状态到半开探查是否重试超时
		if b.retryTimeoutArrived() && b.fromOpenToHalfOpen(ctx) {
			return true
		}
	}
	return false
}

// OnRequestComplete 请求结束记录rt
func (b *slowRtCircuitBreaker) OnRequestComplete(rt uint64, _ error) {
	// add slow and add total
	metricStat := b.stat
	counter, curErr := metricStat.currentCounter()
	if curErr != nil {
		logging.Error(curErr, "Fail to get current counter in slowRtCircuitBreaker#OnRequestComplete().",
			"rule", b.rule)
		return
	}
	// 超过慢请求设置的进行记录
	if rt > b.maxAllowedRt {
		atomic.AddUint64(&counter.slowCount, 1)
	}
	atomic.AddUint64(&counter.totalCount, 1)

	// 当前时间下总的慢请求数，总请求数
	slowCount := uint64(0)
	totalCount := uint64(0)
	counters := metricStat.allCounter()
	for _, c := range counters {
		slowCount += atomic.LoadUint64(&c.slowCount)
		totalCount += atomic.LoadUint64(&c.totalCount)
	}

	// 计算慢请求比例
	slowRatio := float64(slowCount) / float64(totalCount)

	// handleStateChange
	// 状态变更 open状态不处理
	curStatus := b.CurrentState()
	if curStatus == Open {
		return //open状态拒绝所有请求，不需要处理
	} else if curStatus == HalfOpen { // 半开状态 探测的流量
		if rt > b.maxAllowedRt {
			// fail to probe 探测失败
			b.fromHalfOpenToOpen(1.0)
		} else {
			// succeed to probe 探测成功
			b.fromHalfOpenToClosed()
			b.resetMetric()
		}
		return
	}

	// current state is CLOSED
	// 关闭的状态校验是否打开
	if totalCount < b.minRequestAmount {
		return
	}

	if slowRatio > b.maxSlowRequestRatio || util.Float64Equals(slowRatio, b.maxSlowRequestRatio) {
		curStatus = b.CurrentState()
		switch curStatus {
		case Closed:
			b.fromClosedToOpen(slowRatio)
		case HalfOpen:
			b.fromHalfOpenToOpen(slowRatio)
		default:
		}
	}
	return
}

// 重置当前的统计信息
func (b *slowRtCircuitBreaker) resetMetric() {
	for _, c := range b.stat.allCounter() {
		c.reset()
	}
}

// 具体的数据统计
type slowRequestCounter struct {
	slowCount  uint64 // 慢请求数
	totalCount uint64 // 总请求数
}

func (c *slowRequestCounter) reset() {
	atomic.StoreUint64(&c.slowCount, 0)
	atomic.StoreUint64(&c.totalCount, 0)
}

// 慢请求的滑动窗口
type slowRequestLeapArray struct {
	data *sbase.LeapArray
}

// NewEmptyBucket 创建空的bucket
func (s *slowRequestLeapArray) NewEmptyBucket() interface{} {
	return &slowRequestCounter{
		slowCount:  0,
		totalCount: 0,
	}
}

// ResetBucketTo bucket重置
func (s *slowRequestLeapArray) ResetBucketTo(bw *sbase.BucketWrap, startTime uint64) *sbase.BucketWrap {
	atomic.StoreUint64(&bw.BucketStart, startTime)
	bw.Value.Store(&slowRequestCounter{
		slowCount:  0,
		totalCount: 0,
	})
	return bw
}

// 获取当前时间bucket的统计数据
func (s *slowRequestLeapArray) currentCounter() (*slowRequestCounter, error) {
	curBucket, err := s.data.CurrentBucket(s)
	if err != nil {
		return nil, err
	}
	if curBucket == nil {
		return nil, errors.New("nil BucketWrap")
	}
	mb := curBucket.Value.Load()
	if mb == nil {
		return nil, errors.New("nil slowRequestCounter")
	}
	counter, ok := mb.(*slowRequestCounter)
	if !ok {
		return nil, errors.Errorf("bucket fail to do type assert, expect: *slowRequestCounter, in fact: %s", reflect.TypeOf(mb).Name())
	}
	return counter, nil
}

// 当前时间所有有效的bucket的统计数据
func (s *slowRequestLeapArray) allCounter() []*slowRequestCounter {
	buckets := s.data.Values()
	ret := make([]*slowRequestCounter, 0, len(buckets))
	for _, b := range buckets {
		mb := b.Value.Load()
		if mb == nil {
			logging.Error(errors.New("current bucket atomic Value is nil"), "Current bucket atomic Value is nil in slowRequestLeapArray.allCounter()")
			continue
		}
		counter, ok := mb.(*slowRequestCounter)
		if !ok {
			logging.Error(errors.New("bucket data type error"), "Bucket data type error in slowRequestLeapArray.allCounter()", "expect type", "*slowRequestCounter", "actual type", reflect.TypeOf(mb).Name())
			continue
		}
		ret = append(ret, counter)
	}
	return ret
}

//================================= errorRatioCircuitBreaker ====================================
// 异常比例breaker
type errorRatioCircuitBreaker struct {
	circuitBreakerBase
	minRequestAmount    uint64  // 可以触发断路的最小请求数
	errorRatioThreshold float64 // 阀值

	stat *errorCounterLeapArray
}

func newErrorRatioCircuitBreakerWithStat(r *Rule, stat *errorCounterLeapArray) *errorRatioCircuitBreaker {
	return &errorRatioCircuitBreaker{
		circuitBreakerBase: circuitBreakerBase{
			rule:                 r,
			retryTimeoutMs:       r.RetryTimeoutMs,
			nextRetryTimestampMs: 0,
			state:                newState(),
		},
		minRequestAmount:    r.MinRequestAmount,
		errorRatioThreshold: r.Threshold,
		stat:                stat,
	}
}

func newErrorRatioCircuitBreaker(r *Rule) (*errorRatioCircuitBreaker, error) {
	interval := r.StatIntervalMs
	bucketCount := getRuleStatSlidingWindowBucketCount(r)
	stat := &errorCounterLeapArray{}
	leapArray, err := sbase.NewLeapArray(bucketCount, interval, stat)
	if err != nil {
		return nil, err
	}
	stat.data = leapArray
	return newErrorRatioCircuitBreakerWithStat(r, stat), nil
}

func (b *errorRatioCircuitBreaker) BoundStat() interface{} {
	return b.stat
}

func (b *errorRatioCircuitBreaker) TryPass(ctx *base.EntryContext) bool {
	curStatus := b.CurrentState()
	if curStatus == Closed {
		return true
	} else if curStatus == Open {
		// switch state to half-open to probe if retry timeout
		if b.retryTimeoutArrived() && b.fromOpenToHalfOpen(ctx) {
			return true
		}
	}
	return false
}

func (b *errorRatioCircuitBreaker) OnRequestComplete(_ uint64, err error) {
	metricStat := b.stat
	counter, curErr := metricStat.currentCounter()
	if curErr != nil {
		logging.Error(curErr, "Fail to get current counter in errorRatioCircuitBreaker#OnRequestComplete().",
			"rule", b.rule)
		return
	}
	if err != nil {
		atomic.AddUint64(&counter.errorCount, 1)
	}
	atomic.AddUint64(&counter.totalCount, 1)

	errorCount := uint64(0)
	totalCount := uint64(0)
	counters := metricStat.allCounter()
	for _, c := range counters {
		errorCount += atomic.LoadUint64(&c.errorCount)
		totalCount += atomic.LoadUint64(&c.totalCount)
	}
	errorRatio := float64(errorCount) / float64(totalCount)

	// handleStateChangeWhenThresholdExceeded
	curStatus := b.CurrentState()
	if curStatus == Open {
		return
	}
	if curStatus == HalfOpen {
		if err == nil {
			b.fromHalfOpenToClosed()
			b.resetMetric()
		} else {
			b.fromHalfOpenToOpen(1.0)
		}
		return
	}

	// current state is CLOSED
	if totalCount < b.minRequestAmount {
		return
	}
	if errorRatio > b.errorRatioThreshold || util.Float64Equals(errorRatio, b.errorRatioThreshold) {
		curStatus = b.CurrentState()
		switch curStatus {
		case Closed:
			b.fromClosedToOpen(errorRatio)
		case HalfOpen:
			b.fromHalfOpenToOpen(errorRatio)
		default:
		}
	}
}

func (b *errorRatioCircuitBreaker) resetMetric() {
	for _, c := range b.stat.allCounter() {
		c.reset()
	}
}

type errorCounter struct {
	errorCount uint64
	totalCount uint64
}

func (c *errorCounter) reset() {
	atomic.StoreUint64(&c.errorCount, 0)
	atomic.StoreUint64(&c.totalCount, 0)
}

// 异常比例的统计
type errorCounterLeapArray struct {
	data *sbase.LeapArray
}

func (s *errorCounterLeapArray) NewEmptyBucket() interface{} {
	return &errorCounter{
		errorCount: 0,
		totalCount: 0,
	}
}

func (s *errorCounterLeapArray) ResetBucketTo(bw *sbase.BucketWrap, startTime uint64) *sbase.BucketWrap {
	atomic.StoreUint64(&bw.BucketStart, startTime)
	bw.Value.Store(&errorCounter{
		errorCount: 0,
		totalCount: 0,
	})
	return bw
}

func (s *errorCounterLeapArray) currentCounter() (*errorCounter, error) {
	curBucket, err := s.data.CurrentBucket(s)
	if err != nil {
		return nil, err
	}
	if curBucket == nil {
		return nil, errors.New("nil BucketWrap")
	}
	mb := curBucket.Value.Load()
	if mb == nil {
		return nil, errors.New("nil errorCounter")
	}
	counter, ok := mb.(*errorCounter)
	if !ok {
		return nil, errors.Errorf("bucket fail to do type assert, expect: *errorCounter, in fact: %s", reflect.TypeOf(mb).Name())
	}
	return counter, nil
}

func (s *errorCounterLeapArray) allCounter() []*errorCounter {
	buckets := s.data.Values()
	ret := make([]*errorCounter, 0, len(buckets))
	for _, b := range buckets {
		mb := b.Value.Load()
		if mb == nil {
			logging.Error(errors.New("current bucket atomic Value is nil"), "Current bucket atomic Value is nil in errorCounterLeapArray.allCounter()")
			continue
		}
		counter, ok := mb.(*errorCounter)
		if !ok {
			logging.Error(errors.New("bucket data type error"), "Bucket data type error in errorCounterLeapArray.allCounter()", "expect type", "*errorCounter", "actual type", reflect.TypeOf(mb).Name())
			continue
		}
		ret = append(ret, counter)
	}
	return ret
}

//================================= errorCountCircuitBreaker ====================================
type errorCountCircuitBreaker struct {
	circuitBreakerBase
	minRequestAmount    uint64
	errorCountThreshold uint64

	stat *errorCounterLeapArray
}

func newErrorCountCircuitBreakerWithStat(r *Rule, stat *errorCounterLeapArray) *errorCountCircuitBreaker {
	return &errorCountCircuitBreaker{
		circuitBreakerBase: circuitBreakerBase{
			rule:                 r,
			retryTimeoutMs:       r.RetryTimeoutMs,
			nextRetryTimestampMs: 0,
			state:                newState(),
		},
		minRequestAmount:    r.MinRequestAmount,
		errorCountThreshold: uint64(r.Threshold),
		stat:                stat,
	}
}

func newErrorCountCircuitBreaker(r *Rule) (*errorCountCircuitBreaker, error) {
	interval := r.StatIntervalMs
	bucketCount := getRuleStatSlidingWindowBucketCount(r)
	stat := &errorCounterLeapArray{}
	leapArray, err := sbase.NewLeapArray(bucketCount, interval, stat)
	if err != nil {
		return nil, err
	}
	stat.data = leapArray
	return newErrorCountCircuitBreakerWithStat(r, stat), nil
}

func (b *errorCountCircuitBreaker) BoundStat() interface{} {
	return b.stat
}

func (b *errorCountCircuitBreaker) TryPass(ctx *base.EntryContext) bool {
	curStatus := b.CurrentState()
	if curStatus == Closed {
		return true
	} else if curStatus == Open {
		// switch state to half-open to probe if retry timeout
		if b.retryTimeoutArrived() && b.fromOpenToHalfOpen(ctx) {
			return true
		}
	}
	return false
}

func (b *errorCountCircuitBreaker) OnRequestComplete(_ uint64, err error) {
	metricStat := b.stat
	counter, curErr := metricStat.currentCounter()
	if curErr != nil {
		logging.Error(curErr, "Fail to get current counter in errorCountCircuitBreaker#OnRequestComplete().",
			"rule", b.rule)
		return
	}
	if err != nil {
		atomic.AddUint64(&counter.errorCount, 1)
	}
	atomic.AddUint64(&counter.totalCount, 1)

	errorCount := uint64(0)
	totalCount := uint64(0)
	counters := metricStat.allCounter()
	for _, c := range counters {
		errorCount += atomic.LoadUint64(&c.errorCount)
		totalCount += atomic.LoadUint64(&c.totalCount)
	}
	// handleStateChangeWhenThresholdExceeded
	curStatus := b.CurrentState()
	if curStatus == Open {
		return
	}
	if curStatus == HalfOpen {
		if err == nil {
			b.fromHalfOpenToClosed()
			b.resetMetric()
		} else {
			b.fromHalfOpenToOpen(1)
		}
		return
	}
	// current state is CLOSED
	if totalCount < b.minRequestAmount {
		return
	}
	if errorCount >= b.errorCountThreshold {
		curStatus = b.CurrentState()
		switch curStatus {
		case Closed:
			b.fromClosedToOpen(errorCount)
		case HalfOpen:
			b.fromHalfOpenToOpen(errorCount)
		default:
		}
	}
}

func (b *errorCountCircuitBreaker) resetMetric() {
	for _, c := range b.stat.allCounter() {
		c.reset()
	}
}
