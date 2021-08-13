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

import (
	"container/list"

	"github.com/pkg/errors"
)

// EvictCallback is used to get a callback when a cache entry is evicted
// 用于在缓存项被逐出时获取回调
type EvictCallback func(key interface{}, value interface{})

// LRU implements a non-thread safe fixed size LRU cache
// 实现一个非线程安全的固定大小的LRU缓存
// 最近最少使用
type LRU struct {
	size      int // 链表记录最近数据的最大值
	evictList *list.List
	items     map[interface{}]*list.Element // evictList中的元素的map记录，用来快速搜索
	onEvict   EvictCallback
}

// entry is used to hold a value in the evictList
// 是用来保存一个值在evictList
type entry struct {
	key   interface{}
	value interface{}
}

// NewLRU constructs an LRU of the given size
// 构造一个给定大小的LRU
func NewLRU(size int, onEvict EvictCallback) (*LRU, error) {
	if size <= 0 {
		return nil, errors.New("must provide a positive size")
	}
	c := &LRU{
		size:      size,
		evictList: list.New(),
		items:     make(map[interface{}]*list.Element, 64),
		onEvict:   onEvict,
	}
	return c, nil
}

// Purge is used to completely clear the cache.
// 用于完全清除缓存。
func (c *LRU) Purge() {
	for k, v := range c.items {
		// 触发回调
		if c.onEvict != nil {
			c.onEvict(k, v.Value.(*entry).value)
		}
		// map中删除
		delete(c.items, k)
	}
	// 重置链表
	c.evictList.Init()
}

// Add 添加新元素已存在就直接更新
func (c *LRU) Add(key, value interface{}) {
	// Check for existing item
	if ent, ok := c.items[key]; ok {
		c.evictList.MoveToFront(ent)     // 将ent移动到列表的第一个位置
		ent.Value.(*entry).value = value // 更新值
		return
	}

	// Add new item
	// 添加元素
	ent := &entry{key, value}
	entry := c.evictList.PushFront(ent) // 添加到链表第一个位置
	c.items[key] = entry                // 添加到map

	evict := c.evictList.Len() > c.size
	// Verify size not exceeded
	// 是否超过最大值 添加一个导致的超限 删除一个
	if evict {
		c.removeOldest()
	}
	return
}

// AddIfAbsent adds item only if key is not existed.
// key不存在是进行添加 已存在返回value
func (c *LRU) AddIfAbsent(key interface{}, value interface{}) (priorValue interface{}) {
	if ent, ok := c.items[key]; ok {
		c.evictList.MoveToFront(ent) // 移动到链表头
		if ent.Value == nil {
			return nil
		}
		return ent.Value.(*entry).value
	}

	// Add new item
	// 添加元素
	ent := &entry{key, value}
	entry := c.evictList.PushFront(ent)
	c.items[key] = entry

	evict := c.evictList.Len() > c.size
	// Verify size not exceeded
	if evict {
		c.removeOldest()
	}
	return nil
}

// Get looks up a key's value from the cache.
// 从缓存中查找键的值。
func (c *LRU) Get(key interface{}) (value interface{}, isFound bool) {
	if ent, ok := c.items[key]; ok {
		c.evictList.MoveToFront(ent) // 移动到链表头
		if ent.Value.(*entry) == nil {
			return nil, false
		}
		return ent.Value.(*entry).value, true
	}
	return
}

// Contains checks if a key is in the cache, without updating the recent-ness
// or deleting it for being stale.
// 只检查是否在缓存中
func (c *LRU) Contains(key interface{}) (ok bool) {
	_, ok = c.items[key]
	return ok
}

// Peek returns the key value (or undefined if not found) without updating
// the "recently used"-ness of the key.
// 返回键值(如果没有找到则为undefined)，而不更新键的“最近使用的”属性。
func (c *LRU) Peek(key interface{}) (value interface{}, isFound bool) {
	var ent *list.Element
	if ent, isFound = c.items[key]; isFound {
		return ent.Value.(*entry).value, true
	}
	return nil, isFound
}

// Remove removes the provided key from the cache, returning if the
// key was contained.
// 从缓存中删除指定的key，如果包含就返回true。
func (c *LRU) Remove(key interface{}) (isFound bool) {
	if ent, ok := c.items[key]; ok {
		c.removeElement(ent)
		return true
	}
	return false
}

// RemoveOldest removes the oldest item from the cache.
// 从缓存中删除最旧的项。
func (c *LRU) RemoveOldest() (key interface{}, value interface{}, ok bool) {
	ent := c.evictList.Back() // 链表最后一个元素或nil
	if ent != nil {
		c.removeElement(ent)
		kv := ent.Value.(*entry)
		return kv.key, kv.value, true
	}
	return nil, nil, false
}

// GetOldest returns the oldest entry
// 返回最旧的数据 不更新移动到链表头
func (c *LRU) GetOldest() (key interface{}, value interface{}, ok bool) {
	ent := c.evictList.Back()
	if ent != nil {
		kv := ent.Value.(*entry)
		return kv.key, kv.value, true
	}
	return nil, nil, false
}

// Keys returns a slice of the keys in the cache, from oldest to newest.
// 返回缓存中从最老到最新的键的切片。
func (c *LRU) Keys() []interface{} {
	keys := make([]interface{}, len(c.items))
	i := 0
	// 从后往前循环
	for ent := c.evictList.Back(); ent != nil; ent = ent.Prev() {
		keys[i] = ent.Value.(*entry).key
		i++
	}
	return keys
}

// Len returns the number of items in the cache.
// 返回缓存中的项数。
func (c *LRU) Len() int {
	return c.evictList.Len()
}

// Resize changes the cache size.
// 更改缓存大小。
func (c *LRU) Resize(size int) (evicted int) {
	diff := c.Len() - size
	if diff < 0 {
		diff = 0
	}
	for i := 0; i < diff; i++ {
		c.removeOldest()
	}
	c.size = size
	return diff
}

// removeOldest removes the oldest item from the cache.
// 从缓存中删除最旧的项。
func (c *LRU) removeOldest() {
	ent := c.evictList.Back() // 返回最后一个元素
	if ent != nil {
		c.removeElement(ent)
	}
}

// removeElement is used to remove a given list element from the cache
// removeElement用于从缓存中删除给定的列表元素
func (c *LRU) removeElement(e *list.Element) {
	c.evictList.Remove(e) // 从链表中删除元素e
	if e.Value == nil {
		return
	}
	kv, ok := e.Value.(*entry)
	if !ok {
		return
	}
	delete(c.items, kv.key)

	// 触发回调
	if c.onEvict != nil {
		c.onEvict(kv.key, kv.value)
	}
}
