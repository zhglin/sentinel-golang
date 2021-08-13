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

package cache

// ConcurrentCounterCache cache the hotspot parameter
// ConcurrentCounterCache缓存热点参数的接口
type ConcurrentCounterCache interface {
	// Add add a value to the cache,
	// Updates the "recently used"-ness of the key.
	// 给缓存添加一个值，
	// s更新键的“最近使用”属性。
	Add(key interface{}, value *int64)

	// AddIfAbsent If the key is not existed in the cache, adds a value to the cache then return nil. And updates the "recently used"-ness of the key
	// If the key is already existed in the cache, do nothing and return the prior value
	// 如果该键在缓存中不存在，则向缓存中添加一个值，然后返回nil。并更新密钥的“最近使用的”属性
	// 如果该键在缓存中已经存在，则不做任何操作并返回先前的值
	AddIfAbsent(key interface{}, value *int64) (priorValue *int64)

	// Get returns key's value from the cache and updates the "recently used"-ness of the key.
	// 从缓存中返回key的值，并更新该键“最近使用”的属性。
	Get(key interface{}) (value *int64, isFound bool)

	// Remove removes a key from the cache.
	// Return true if the key was contained.
	// Remove从缓存中删除一个键。如果键被包含，返回true。
	Remove(key interface{}) (isFound bool)

	// Contains checks if a key exists in cache
	// Without updating the recent-ness.
	// 包含检查一个键是否在缓存中存在而不更新“最近使用”的属性。
	Contains(key interface{}) (ok bool)

	// Keys returns a slice of the keys in the cache, from oldest to newest.
	// 返回缓存中从最老到最新的键的切片。
	Keys() []interface{}

	// Len returns the number of items in the cache.
	// 返回缓存中的项数。
	Len() int

	// Purge clears all cache entries.
	// 清除所有缓存项。
	Purge()
}
