package consul

import (
	"fmt"

	"github.com/hashicorp/consul/api"
)

// Put 写入KV
func (c *Client) Put(key string, value []byte) error {
	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}

	pair := &api.KVPair{
		Key:   key,
		Value: value,
	}

	_, err := c.client.KV().Put(pair, nil)
	if err != nil {
		return fmt.Errorf("failed to put value: %v", err)
	}

	c.logger.Printf("Value put for key: %s", key)
	return nil
}

// Get 获取KV
func (c *Client) Get(key string) ([]byte, error) {
	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	pair, _, err := c.client.KV().Get(key, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get value: %v", err)
	}

	if pair == nil {
		return nil, nil
	}

	return pair.Value, nil
}

// Delete 删除KV
func (c *Client) Delete(key string) error {
	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}

	_, err := c.client.KV().Delete(key, nil)
	if err != nil {
		return fmt.Errorf("failed to delete key: %v", err)
	}

	c.logger.Printf("Key deleted: %s", key)
	return nil
}

// List 列出指定前缀的所有KV
func (c *Client) List(prefix string) (map[string][]byte, error) {
	pairs, _, err := c.client.KV().List(prefix, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list keys: %v", err)
	}

	result := make(map[string][]byte)
	for _, pair := range pairs {
		result[pair.Key] = pair.Value
	}

	return result, nil
}

// CAS (Compare-And-Swap) 原子更新操作
func (c *Client) CAS(key string, value []byte, version uint64) (bool, error) {
	if key == "" {
		return false, fmt.Errorf("key cannot be empty")
	}

	pair := &api.KVPair{
		Key:         key,
		Value:       value,
		ModifyIndex: version,
	}

	success, _, err := c.client.KV().CAS(pair, nil)
	if err != nil {
		return false, fmt.Errorf("failed to perform CAS operation: %v", err)
	}

	if success {
		c.logger.Printf("CAS operation successful for key: %s", key)
	} else {
		c.logger.Printf("CAS operation failed for key: %s (version mismatch)", key)
	}

	return success, nil
}

// GetWithOptions 获取KV，支持更多选项
func (c *Client) GetWithOptions(key string, opts *api.QueryOptions) (*api.KVPair, error) {
	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	pair, _, err := c.client.KV().Get(key, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get value: %v", err)
	}

	return pair, nil
}

// PutWithOptions 写入KV，支持更多选项
func (c *Client) PutWithOptions(pair *api.KVPair, opts *api.WriteOptions) error {
	if pair == nil {
		return fmt.Errorf("KV pair cannot be nil")
	}

	if pair.Key == "" {
		return fmt.Errorf("key cannot be empty")
	}

	_, err := c.client.KV().Put(pair, opts)
	if err != nil {
		return fmt.Errorf("failed to put value: %v", err)
	}

	c.logger.Printf("Value put for key: %s", pair.Key)
	return nil
}
