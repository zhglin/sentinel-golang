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

package isolation

import (
	"encoding/json"
	"fmt"
)

// MetricType represents the target metric type.
type MetricType int32

const (
	// Concurrency represents concurrency (in-flight requests).
	Concurrency MetricType = iota
)

func (s MetricType) String() string {
	switch s {
	case Concurrency:
		return "Concurrency"
	default:
		return "Undefined"
	}
}

// Rule describes the isolation policy (e.g. semaphore isolation).
// 并发隔离控制是指基于资源访问的并发协程数来控制对资源的访问，
// 这里的思路和信号量隔离很类似，主要是控制对资源访问的最大并发数，避免因为资源的异常导致协程耗尽。
// 在分布式系统架构中，我们一般推荐在客户端（调用端）做一层软隔离（并发隔离控制），达到对资源访问的并发控制的目的。
type Rule struct {
	// ID represents the unique ID of the rule (optional).
	ID string `json:"id,omitempty"`
	// Resource represents the target resource definition.
	// 资源名
	Resource string `json:"resource"`
	// MetricType indicates the metric type for checking logic.
	// Currently Concurrency is supported for concurrency limiting.
	// 逻辑检查的度量类型。当前只支持并发限制。
	MetricType MetricType `json:"metricType"`
	// 阀值
	Threshold uint32 `json:"threshold"`
}

func (r *Rule) String() string {
	b, err := json.Marshal(r)
	if err != nil {
		// Return the fallback string
		return fmt.Sprintf("{Id=%s, Resource=%s, MetricType=%s, Threshold=%d}", r.ID, r.Resource, r.MetricType.String(), r.Threshold)
	}
	return string(b)
}

func (r *Rule) ResourceName() string {
	return r.Resource
}
