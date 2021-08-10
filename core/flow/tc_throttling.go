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

package flow

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/alibaba/sentinel-golang/core/base"
	"github.com/alibaba/sentinel-golang/util"
)

const (
	BlockMsgQueueing = "flow throttling check blocked, estimated queueing time exceeds max queueing time"

	MillisToNanosOffset = int64(time.Millisecond / time.Nanosecond) // 毫秒转纳秒
)

// ThrottlingChecker limits the time interval between two requests.
// 限制两个请求之间的时间间隔。
type ThrottlingChecker struct {
	owner             *TrafficShapingController
	maxQueueingTimeNs int64
	statIntervalNs    int64
	lastPassedTime    int64 // 最后一次通过的时间
}

func NewThrottlingChecker(owner *TrafficShapingController, timeoutMs uint32, statIntervalMs uint32) *ThrottlingChecker {
	var statIntervalNs int64
	if statIntervalMs == 0 {
		statIntervalNs = 1000 * MillisToNanosOffset
	} else {
		statIntervalNs = int64(statIntervalMs) * MillisToNanosOffset
	}
	return &ThrottlingChecker{
		owner:             owner,
		maxQueueingTimeNs: int64(timeoutMs) * MillisToNanosOffset,
		statIntervalNs:    statIntervalNs,
		lastPassedTime:    0,
	}
}
func (c *ThrottlingChecker) BoundOwner() *TrafficShapingController {
	return c.owner
}

func (c *ThrottlingChecker) DoCheck(_ base.StatNode, batchCount uint32, threshold float64) *base.TokenResult {
	// Pass when batch count is less or equal than 0.
	// 申请的资源数
	if batchCount <= 0 {
		return nil
	}

	var rule *Rule
	if c.BoundOwner() != nil {
		rule = c.BoundOwner().BoundRule()
	}

	// 控制阀值
	if threshold <= 0.0 {
		msg := "flow throttling check blocked, threshold is <= 0.0"
		return base.NewTokenResultBlockedWithCause(base.BlockTypeFlow, msg, rule, nil)
	}

	// 一次申请的资源数超过阀值
	if float64(batchCount) > threshold {
		return base.NewTokenResultBlocked(base.BlockTypeFlow)
	}
	// Here we use nanosecond so that we could control the queueing time more accurately.
	// 这里我们使用纳秒，以便更精确地控制排队时间。
	curNano := int64(util.CurrentTimeNano())

	// The interval between two requests (in nanoseconds).
	// batchCount个请求需要的时间(以纳秒为单位)。
	intervalNs := int64(math.Ceil(float64(batchCount) / threshold * float64(c.statIntervalNs)))

	loadedLastPassedTime := atomic.LoadInt64(&c.lastPassedTime)
	// Expected pass time of this request.
	// 预计通过此请求的时间。
	expectedTime := loadedLastPassedTime + intervalNs
	if expectedTime <= curNano { // 小于当前时间
		// 防止写入时被更改
		if swapped := atomic.CompareAndSwapInt64(&c.lastPassedTime, loadedLastPassedTime, curNano); swapped {
			// nil means pass
			return nil
		}
	}

	// 通过时间超过maxQueueingTimeNs时间
	estimatedQueueingDuration := atomic.LoadInt64(&c.lastPassedTime) + intervalNs - curNano
	if estimatedQueueingDuration > c.maxQueueingTimeNs {
		return base.NewTokenResultBlockedWithCause(base.BlockTypeFlow, BlockMsgQueueing, rule, nil)
	}

	// 未超过maxQueueingTimeNs时间 计算wait时间
	oldTime := atomic.AddInt64(&c.lastPassedTime, intervalNs)
	estimatedQueueingDuration = oldTime - curNano
	if estimatedQueueingDuration > c.maxQueueingTimeNs { // 并发导致不一致 再次校验
		// Subtract the interval.
		atomic.AddInt64(&c.lastPassedTime, -intervalNs)
		return base.NewTokenResultBlockedWithCause(base.BlockTypeFlow, BlockMsgQueueing, rule, nil)
	}
	if estimatedQueueingDuration > 0 {
		return base.NewTokenResultShouldWait(time.Duration(estimatedQueueingDuration))
	} else {
		return base.NewTokenResultShouldWait(0)
	}
}
