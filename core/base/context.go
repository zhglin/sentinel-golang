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

import "github.com/alibaba/sentinel-golang/util"

type EntryContext struct {
	entry *SentinelEntry
	// internal error when sentinel Entry or
	// biz error of downstream
	// sentinel输入时出现内部错误或下游业务错误
	err error
	// Use to calculate RT
	startTime uint64 // 创建EntryContext的时间

	// the rt of this transaction
	rt uint64 // 响应时间

	Resource *ResourceWrapper // 资源信息
	StatNode StatNode         // 滑动窗口统计信息

	Input *SentinelInput
	// the result of rule slots check
	// 检查的结果
	RuleCheckResult *TokenResult
	// reserve for storing some intermediate data from the Entry execution process
	Data map[interface{}]interface{}
}

func (ctx *EntryContext) SetEntry(entry *SentinelEntry) {
	ctx.entry = entry
}

func (ctx *EntryContext) Entry() *SentinelEntry {
	return ctx.entry
}

func (ctx *EntryContext) Err() error {
	return ctx.err
}

func (ctx *EntryContext) SetError(err error) {
	ctx.err = err
}

func (ctx *EntryContext) StartTime() uint64 {
	return ctx.startTime
}

// IsBlocked 执行结果是否blocked
func (ctx *EntryContext) IsBlocked() bool {
	if ctx.RuleCheckResult == nil {
		return false
	}
	return ctx.RuleCheckResult.IsBlocked()
}

// PutRt 写入执行时间
func (ctx *EntryContext) PutRt(rt uint64) {
	ctx.rt = rt
}

// Rt 获取并写入执行时间
func (ctx *EntryContext) Rt() uint64 {
	if ctx.rt == 0 {
		rt := util.CurrentTimeMillis() - ctx.StartTime()
		return rt
	}
	return ctx.rt
}

func NewEmptyEntryContext() *EntryContext {
	return &EntryContext{}
}

// The input data of sentinel
type SentinelInput struct {
	BatchCount uint32 // 资源申请的数量
	Flag       int32
	Args       []interface{} // 热点参数流控的参数
	// store some values in this context when calling context in slot.
	// 当调用slot中的context时，在这个上下文中存储一些值。
	Attachments map[interface{}]interface{}
}

func (i *SentinelInput) reset() {
	i.BatchCount = 1
	i.Flag = 0
	if len(i.Args) != 0 {
		i.Args = make([]interface{}, 0)
	}
	if len(i.Attachments) != 0 {
		i.Attachments = make(map[interface{}]interface{})
	}
}

// Reset init EntryContext,
func (ctx *EntryContext) Reset() {
	// reset all fields of ctx
	ctx.entry = nil
	ctx.err = nil
	ctx.startTime = 0
	ctx.rt = 0
	ctx.Resource = nil
	ctx.StatNode = nil
	ctx.Input.reset()
	if ctx.RuleCheckResult == nil {
		ctx.RuleCheckResult = NewTokenResultPass()
	} else {
		ctx.RuleCheckResult.ResetToPass()
	}
	if len(ctx.Data) != 0 {
		ctx.Data = make(map[interface{}]interface{})
	}
}
