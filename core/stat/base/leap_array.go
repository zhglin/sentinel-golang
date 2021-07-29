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
	"fmt"
	"runtime"
	"sync/atomic"
	"unsafe"

	"github.com/alibaba/sentinel-golang/core/base"
	"github.com/alibaba/sentinel-golang/logging"
	"github.com/alibaba/sentinel-golang/util"
	"github.com/pkg/errors"
)

const (
	PtrSize = int(8)
)

// BucketWrap represent a slot to record metrics
// In order to reduce the usage of memory, BucketWrap don't hold length of BucketWrap
// The length of BucketWrap could be seen in LeapArray.
// The scope of time is [startTime, startTime+bucketLength)
// The size of BucketWrap is 24(8+16) bytes
// BucketWrap表示一个记录度量的槽
// 为了减少内存的使用，BucketWrap不保存BucketWrap的长度
// 在LeapArray中可以看到bucket twrap的长度。
// 时间范围为[startTime, startTime+bucketLength]
// bucket twrap的大小为24(8+16)字节
type BucketWrap struct {
	// The start timestamp of this statistic bucket wrapper.
	// 当前bucket的起始时间
	BucketStart uint64
	// The actual data structure to record the metrics (e.g. MetricBucket).
	// 当前bucket时间段内的统计值
	Value atomic.Value
}

func (ww *BucketWrap) resetTo(startTime uint64) {
	ww.BucketStart = startTime
}

func (ww *BucketWrap) isTimeInBucket(now uint64, bucketLengthInMs uint32) bool {
	return ww.BucketStart <= now && now < ww.BucketStart+uint64(bucketLengthInMs)
}

// 重新计算当前时间 正好是每个bucket时长的倍数
func calculateStartTime(now uint64, bucketLengthInMs uint32) uint64 {
	return now - (now % uint64(bucketLengthInMs))
}

// AtomicBucketWrapArray atomic BucketWrap array to resolve race condition
// AtomicBucketWrapArray can not append or delete element after initializing
// AtomicBucketWrapArray在初始化后不能追加或删除元素
type AtomicBucketWrapArray struct {
	// The base address for real data array
	// data数组的起始地址
	base unsafe.Pointer
	// The length of slice(array), it can not be modified.
	length int // data的数据长度
	data   []*BucketWrap
}

// NewAtomicBucketWrapArrayWithTime 具体的创建AtomicBucketWrapArray的函数
func NewAtomicBucketWrapArrayWithTime(len int, bucketLengthInMs uint32, now uint64, generator BucketGenerator) *AtomicBucketWrapArray {
	ret := &AtomicBucketWrapArray{
		length: len,
		data:   make([]*BucketWrap, len),
	}

	idx := int((now / uint64(bucketLengthInMs)) % uint64(len)) // 当前时间在data中的下标
	startTime := calculateStartTime(now, bucketLengthInMs)     // 重新计算当前时间

	// 初始化idx之后的data
	for i := idx; i <= len-1; i++ {
		ww := &BucketWrap{
			BucketStart: startTime,
			Value:       atomic.Value{},
		}
		ww.Value.Store(generator.NewEmptyBucket())
		ret.data[i] = ww
		startTime += uint64(bucketLengthInMs)
	}

	// 初始化idx之前的data 开始时间累加
	for i := 0; i < idx; i++ {
		ww := &BucketWrap{
			BucketStart: startTime,
			Value:       atomic.Value{},
		}
		ww.Value.Store(generator.NewEmptyBucket()) // 创建bucketWrap的Value
		ret.data[i] = ww
		startTime += uint64(bucketLengthInMs)
	}

	// calculate base address for real data array
	sliHeader := (*util.SliceHeader)(unsafe.Pointer(&ret.data))
	ret.base = unsafe.Pointer((**BucketWrap)(unsafe.Pointer(sliHeader.Data)))
	return ret
}

// NewAtomicBucketWrapArray New AtomicBucketWrapArray with initializing field data
// Default, automatically initialize each BucketWrap
// len: length of array
// bucketLengthInMs: bucket length of BucketWrap
// generator: generator to generate bucket
// 默认情况下，自动初始化每个BucketWrap
// len:数组长度
// bucketLengthInMs: buckettwrap的时间长度
// generator:生成桶的发电机
func NewAtomicBucketWrapArray(len int, bucketLengthInMs uint32, generator BucketGenerator) *AtomicBucketWrapArray {
	return NewAtomicBucketWrapArrayWithTime(len, bucketLengthInMs, util.CurrentTimeMillis(), generator)
}

// 校验并返回给定idx的内存地址
func (aa *AtomicBucketWrapArray) elementOffset(idx int) (unsafe.Pointer, bool) {
	if idx >= aa.length || idx < 0 {
		logging.Error(errors.New("array index out of bounds"),
			"array index out of bounds in AtomicBucketWrapArray.elementOffset()",
			"idx", idx, "arrayLength", aa.length)
		return nil, false
	}
	basePtr := aa.base
	return unsafe.Pointer(uintptr(basePtr) + uintptr(idx*PtrSize)), true
}

// 返回指定的idx的bucket
func (aa *AtomicBucketWrapArray) get(idx int) *BucketWrap {
	// aa.elementOffset(idx) return the secondary pointer of BucketWrap, which is the pointer to the aa.data[idx]
	// then convert to (*unsafe.Pointer)
	if offset, ok := aa.elementOffset(idx); ok {
		return (*BucketWrap)(atomic.LoadPointer((*unsafe.Pointer)(offset)))
	}
	return nil
}

func (aa *AtomicBucketWrapArray) compareAndSet(idx int, except, update *BucketWrap) bool {
	// aa.elementOffset(idx) return the secondary pointer of BucketWrap, which is the pointer to the aa.data[idx]
	// then convert to (*unsafe.Pointer)
	// update secondary pointer
	if offset, ok := aa.elementOffset(idx); ok {
		return atomic.CompareAndSwapPointer((*unsafe.Pointer)(offset), unsafe.Pointer(except), unsafe.Pointer(update))
	}
	return false
}

// The BucketWrap leap array,
// sampleCount represent the number of BucketWrap
// intervalInMs represent the interval of LeapArray.
// For example, bucketLengthInMs is 200ms, intervalInMs is 1000ms, so sampleCount is 5.
// Give a diagram to illustrate
// Suppose current time is 888, bucketLengthInMs is 200ms, intervalInMs is 1000ms, LeapArray will build the below windows
//   B0       B1      B2     B3      B4
//   |_______|_______|_______|_______|_______|
//  1000    1200    1400    1600    800    (1000)
//                                        ^
//                                      time=888
type LeapArray struct {
	bucketLengthInMs uint32 // 一个bucket的时间长度
	sampleCount      uint32 // 窗口的总bucket数量
	intervalInMs     uint32 // 窗口的总时长
	array            *AtomicBucketWrapArray
	// update lock
	updateLock mutex
}

func NewLeapArray(sampleCount uint32, intervalInMs uint32, generator BucketGenerator) (*LeapArray, error) {
	if sampleCount == 0 || intervalInMs%sampleCount != 0 {
		return nil, errors.Errorf("Invalid parameters, intervalInMs is %d, sampleCount is %d", intervalInMs, sampleCount)
	}
	if generator == nil {
		return nil, errors.Errorf("Invalid parameters, BucketGenerator is nil")
	}
	bucketLengthInMs := intervalInMs / sampleCount
	return &LeapArray{
		bucketLengthInMs: bucketLengthInMs,
		sampleCount:      sampleCount,
		intervalInMs:     intervalInMs,
		array:            NewAtomicBucketWrapArray(int(sampleCount), bucketLengthInMs, generator),
	}, nil
}

func (la *LeapArray) CurrentBucket(bg BucketGenerator) (*BucketWrap, error) {
	return la.currentBucketOfTime(util.CurrentTimeMillis(), bg)
}

func (la *LeapArray) currentBucketOfTime(now uint64, bg BucketGenerator) (*BucketWrap, error) {
	if now <= 0 {
		return nil, errors.New("Current time is less than 0.")
	}

	idx := la.calculateTimeIdx(now)
	bucketStart := calculateStartTime(now, la.bucketLengthInMs)

	for { //spin to get the current BucketWrap
		old := la.array.get(idx)
		if old == nil {
			// because la.array.data had initiated when new la.array
			// theoretically, here is not reachable
			newWrap := &BucketWrap{
				BucketStart: bucketStart,
				Value:       atomic.Value{},
			}
			newWrap.Value.Store(bg.NewEmptyBucket())
			if la.array.compareAndSet(idx, nil, newWrap) {
				return newWrap, nil
			} else {
				runtime.Gosched()
			}
		} else if bucketStart == atomic.LoadUint64(&old.BucketStart) {
			return old, nil
		} else if bucketStart > atomic.LoadUint64(&old.BucketStart) {
			// current time has been next cycle of LeapArray and LeapArray dont't count in last cycle.
			// reset BucketWrap
			if la.updateLock.TryLock() {
				old = bg.ResetBucketTo(old, bucketStart)
				la.updateLock.Unlock()
				return old, nil
			} else {
				runtime.Gosched()
			}
		} else if bucketStart < atomic.LoadUint64(&old.BucketStart) {
			if la.sampleCount == 1 {
				// if sampleCount==1 in leap array, in concurrency scenario, this case is possible
				return old, nil
			}
			// TODO: reserve for some special case (e.g. when occupying "future" buckets).
			return nil, errors.New(fmt.Sprintf("Provided time timeMillis=%d is already behind old.BucketStart=%d.", bucketStart, old.BucketStart))
		}
	}
}

func (la *LeapArray) calculateTimeIdx(now uint64) int {
	timeId := now / uint64(la.bucketLengthInMs)
	return int(timeId) % la.array.length
}

//  Get all BucketWrap between [current time - leap array interval, current time]
func (la *LeapArray) Values() []*BucketWrap {
	return la.valuesWithTime(util.CurrentTimeMillis())
}

func (la *LeapArray) valuesWithTime(now uint64) []*BucketWrap {
	if now <= 0 {
		return make([]*BucketWrap, 0)
	}
	ret := make([]*BucketWrap, 0, la.array.length)
	for i := 0; i < la.array.length; i++ {
		ww := la.array.get(i)
		if ww == nil || la.isBucketDeprecated(now, ww) {
			continue
		}
		ret = append(ret, ww)
	}
	return ret
}

// ValuesConditional 根据predicate函数刷选bucket
func (la *LeapArray) ValuesConditional(now uint64, predicate base.TimePredicate) []*BucketWrap {
	if now <= 0 {
		return make([]*BucketWrap, 0)
	}
	ret := make([]*BucketWrap, 0, la.array.length)
	for i := 0; i < la.array.length; i++ {
		ww := la.array.get(i)
		if ww == nil || la.isBucketDeprecated(now, ww) || !predicate(atomic.LoadUint64(&ww.BucketStart)) {
			continue
		}
		ret = append(ret, ww)
	}
	return ret
}

// Judge whether the BucketWrap is expired
// 判断BucketWrap是否过期 是否在窗口内
func (la *LeapArray) isBucketDeprecated(now uint64, ww *BucketWrap) bool {
	ws := atomic.LoadUint64(&ww.BucketStart)
	return (now - ws) > uint64(la.intervalInMs)
}

// BucketGenerator Generic interface to generate bucket
// 生成桶的bucket的通用接口
type BucketGenerator interface {
	// NewEmptyBucket called when timestamp entry a new slot interval
	// 当时间戳条目为新的槽间隔时调用
	NewEmptyBucket() interface{}

	// ResetBucketTo reset the BucketWrap, clear all data of BucketWrap
	// 重置BucketWrap，清除所有BucketWrap数据
	ResetBucketTo(bw *BucketWrap, startTime uint64) *BucketWrap
}
