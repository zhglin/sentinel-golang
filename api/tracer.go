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

package api

import (
	"github.com/alibaba/sentinel-golang/core/base"
	"github.com/alibaba/sentinel-golang/logging"
	"github.com/pkg/errors"
)

// TraceError records the provided error to the given SentinelEntry.
// 将提供的错误记录到给定的SentinelEntry。
// 这里的错误比例熔断和错误计数熔断指的业务返回错误的比例或则计数。
// 也就是说，如果规则指定熔断器策略采用错误比例或则错误计数，那么为了统计错误比例或错误计数，需要调用TraceError(entry, err) 埋点每个请求的业务异常。
func TraceError(entry *base.SentinelEntry, err error) {
	defer func() {
		if e := recover(); e != nil {
			logging.Error(errors.Errorf("%+v", e), "Failed to api.TraceError()")
			return
		}
	}()
	if entry == nil || err == nil {
		return
	}

	entry.SetError(err)
}
