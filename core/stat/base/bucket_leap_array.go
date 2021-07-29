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

package base

import (
	"reflect"
	"sync/atomic"

	"github.com/alibaba/sentinel-golang/core/base"
	"github.com/alibaba/sentinel-golang/logging"
	"github.com/alibaba/sentinel-golang/util"
	"github.com/pkg/errors"
)

// BucketLeapArray The implementation of sliding window based on LeapArray (as the sliding window infrastructure)
// and MetricBucket (as the data type). The MetricBucket is used to record statistic
// metrics per minimum time unit (i.e. the bucket time span).
// 基于LeapArray(作为滑动窗口基础设施)和MetricBucket(作为数据类型)实现滑动窗口。
// MetricBucket用于记录每个最小时间单位(即桶的时间跨度)的统计指标。
type BucketLeapArray struct {
	data     LeapArray // 滑动窗口
	dataType string    // 数据类型 固定的值(MetricBucket)
}

// NewEmptyBucket 创建新的bucket的函数
func (bla *BucketLeapArray) NewEmptyBucket() interface{} {
	return NewMetricBucket()
}

func (bla *BucketLeapArray) ResetBucketTo(bw *BucketWrap, startTime uint64) *BucketWrap {
	atomic.StoreUint64(&bw.BucketStart, startTime)
	bw.Value.Store(NewMetricBucket())
	return bw
}

// NewBucketLeapArray sampleCount is the number of slots
// intervalInMs is the time length of sliding window
// sampleCount and intervalInMs must be positive and intervalInMs%sampleCount == 0,
// the validation must be done before call NewBucketLeapArray
// sampleCount是槽的数量
// intervalInMs为滑动窗口的时间长度
// sampleCount和intervalInMs必须是正的，并且intervalInMs%sampleCount == 0，
// 验证必须在调用NewBucketLeapArray之前完成
func NewBucketLeapArray(sampleCount uint32, intervalInMs uint32) *BucketLeapArray {
	bucketLengthInMs := intervalInMs / sampleCount // 一个bucket的时间长度
	ret := &BucketLeapArray{
		data: LeapArray{
			bucketLengthInMs: bucketLengthInMs,
			sampleCount:      sampleCount,
			intervalInMs:     intervalInMs,
			array:            nil,
		},
		dataType: "MetricBucket",
	}
	arr := NewAtomicBucketWrapArray(int(sampleCount), bucketLengthInMs, ret)
	ret.data.array = arr
	return ret
}

func (bla *BucketLeapArray) SampleCount() uint32 {
	return bla.data.sampleCount
}

func (bla *BucketLeapArray) IntervalInMs() uint32 {
	return bla.data.intervalInMs
}

func (bla *BucketLeapArray) BucketLengthInMs() uint32 {
	return bla.data.bucketLengthInMs
}

func (bla *BucketLeapArray) DataType() string {
	return bla.dataType
}

func (bla *BucketLeapArray) GetIntervalInSecond() float64 {
	return float64(bla.IntervalInMs()) / 1000.0
}

// Write method
// It might panic
func (bla *BucketLeapArray) AddCount(event base.MetricEvent, count int64) {
	bla.addCountWithTime(util.CurrentTimeMillis(), event, count)
}

func (bla *BucketLeapArray) addCountWithTime(now uint64, event base.MetricEvent, count int64) {
	b := bla.currentBucketWithTime(now)
	if b == nil {
		return
	}
	b.Add(event, count)
}

func (bla *BucketLeapArray) UpdateConcurrency(concurrency int32) {
	bla.updateConcurrencyWithTime(util.CurrentTimeMillis(), concurrency)
}

func (bla *BucketLeapArray) updateConcurrencyWithTime(now uint64, concurrency int32) {
	b := bla.currentBucketWithTime(now)
	if b == nil {
		return
	}
	b.UpdateConcurrency(concurrency)
}

func (bla *BucketLeapArray) currentBucketWithTime(now uint64) *MetricBucket {
	curBucket, err := bla.data.currentBucketOfTime(now, bla)
	if err != nil {
		logging.Error(err, "Failed to get current bucket in BucketLeapArray.currentBucketWithTime()", "now", now)
		return nil
	}
	if curBucket == nil {
		logging.Error(errors.New("current bucket is nil"), "Nil curBucket in BucketLeapArray.currentBucketWithTime()")
		return nil
	}
	mb := curBucket.Value.Load()
	if mb == nil {
		logging.Error(errors.New("nil bucket"), "Current bucket atomic Value is nil in BucketLeapArray.currentBucketWithTime()")
		return nil
	}
	b, ok := mb.(*MetricBucket)
	if !ok {
		logging.Error(errors.New("fail to type assert"), "Bucket data type error in BucketLeapArray.currentBucketWithTime()", "expectType", "*MetricBucket", "actualType", reflect.TypeOf(mb).Name())
		return nil
	}
	return b
}

// Read method, need to adapt upper application
// it might panic
func (bla *BucketLeapArray) Count(event base.MetricEvent) int64 {
	return bla.CountWithTime(util.CurrentTimeMillis(), event)
}

func (bla *BucketLeapArray) CountWithTime(now uint64, event base.MetricEvent) int64 {
	_, err := bla.data.currentBucketOfTime(now, bla)
	if err != nil {
		logging.Error(err, "Failed to get current bucket in BucketLeapArray.CountWithTime()", "now", now)
	}
	count := int64(0)
	for _, ww := range bla.data.valuesWithTime(now) {
		mb := ww.Value.Load()
		if mb == nil {
			logging.Error(errors.New("current bucket is nil"), "Failed to load current bucket in BucketLeapArray.CountWithTime()")
			continue
		}
		b, ok := mb.(*MetricBucket)
		if !ok {
			logging.Error(errors.New("fail to type assert"), "Bucket data type error in BucketLeapArray.CountWithTime()", "expectType", "*MetricBucket", "actualType", reflect.TypeOf(mb).Name())
			continue
		}
		count += b.Get(event)
	}
	return count
}

// Read method, get all BucketWrap.
func (bla *BucketLeapArray) Values(now uint64) []*BucketWrap {
	_, err := bla.data.currentBucketOfTime(now, bla)
	if err != nil {
		logging.Error(err, "Failed to get current bucket in BucketLeapArray.Values()", "now", now)
	}
	return bla.data.valuesWithTime(now)
}

// ValuesConditional 根据predicate函数返回符合条件的bucket
func (bla *BucketLeapArray) ValuesConditional(now uint64, predicate base.TimePredicate) []*BucketWrap {
	return bla.data.ValuesConditional(now, predicate)
}

func (bla *BucketLeapArray) MinRt() int64 {
	_, err := bla.data.CurrentBucket(bla)
	if err != nil {
		logging.Error(err, "Failed to get current bucket in BucketLeapArray.MinRt()")
	}

	ret := base.DefaultStatisticMaxRt

	for _, v := range bla.data.Values() {
		mb := v.Value.Load()
		if mb == nil {
			logging.Error(errors.New("current bucket is nil"), "Failed to load current bucket in BucketLeapArray.MinRt()")
			continue
		}
		b, ok := mb.(*MetricBucket)
		if !ok {
			logging.Error(errors.New("fail to type assert"), "Bucket data type error in BucketLeapArray.MinRt()", "expectType", "*MetricBucket", "actualType", reflect.TypeOf(mb).Name())
			continue
		}
		mr := b.MinRt()
		if ret > mr {
			ret = mr
		}
	}
	return ret
}

func (bla *BucketLeapArray) MaxConcurrency() int32 {
	_, err := bla.data.CurrentBucket(bla)
	if err != nil {
		logging.Error(err, "Failed to get current bucket in BucketLeapArray.MaxConcurrency()")
	}

	ret := int32(0)

	for _, v := range bla.data.Values() {
		mb := v.Value.Load()
		if mb == nil {
			logging.Error(errors.New("current bucket is nil"), "Failed to load current bucket in BucketLeapArray.MaxConcurrency()")
			continue
		}
		b, ok := mb.(*MetricBucket)
		if !ok {
			logging.Error(errors.New("fail to type assert"), "Bucket data type error in BucketLeapArray.MaxConcurrency()", "expectType", "*MetricBucket", "actualType", reflect.TypeOf(mb).Name())
			continue
		}
		mc := b.MaxConcurrency()
		if ret < mc {
			ret = mc
		}
	}
	return ret
}
