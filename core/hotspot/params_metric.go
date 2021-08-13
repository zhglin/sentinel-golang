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

package hotspot

import "github.com/alibaba/sentinel-golang/core/hotspot/cache"

const (
	ConcurrencyMaxCount = 4000
	ParamsCapacityBase  = 4000
	ParamsMaxCapacity   = 20000
)

// ParamsMetric carries real-time counters for frequent ("hot spot") parameters.
// 携带频繁(“热点”)参数的实时计数器。
// For each cache map, the key is the parameter value, while the value is the counter.
// 对于每个缓存映射，键是参数值，而值是计数器。
type ParamsMetric struct {
	// RuleTimeCounter records the last added token timestamp.
	// 记录最后添加的令牌时间戳。
	RuleTimeCounter cache.ConcurrentCounterCache
	// RuleTokenCounter records the number of tokens.
	// 记录令牌的数量。
	RuleTokenCounter cache.ConcurrentCounterCache
	// ConcurrencyCounter records the real-time concurrency.
	// 记录实时并发数。
	ConcurrencyCounter cache.ConcurrentCounterCache
}
