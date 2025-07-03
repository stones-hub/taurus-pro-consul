// watch.go
package consul

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hashicorp/consul/api"
)

// WatchOptions 监听选项
type WatchOptions struct {
	WaitTime  time.Duration // 等待时间
	RetryTime time.Duration // 重试间隔
}

// WatchConfig 监听配置并自动解析到结构体
func (c *Client) WatchConfig(key string, config interface{}, opts *WatchOptions) error {
	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}

	if opts == nil {
		opts = &WatchOptions{
			WaitTime:  time.Second * 10,
			RetryTime: time.Second,
		}
	}

	// 先获取初始配置
	pair, _, err := c.client.KV().Get(key, nil)
	if err != nil {
		return fmt.Errorf("failed to get initial config: %v", err)
	}
	if pair != nil {
		if err := json.Unmarshal(pair.Value, config); err != nil {
			return fmt.Errorf("failed to parse initial config: %v", err)
		}
	}

	// 启动监听
	go func() {
		var waitIndex uint64
		for {
			select {
			case <-c.ctx.Done():
				c.logger.Printf("Stopping watch for key: %s", key)
				return
			default:
				pair, meta, err := c.client.KV().Get(key, &api.QueryOptions{
					WaitIndex: waitIndex,
					WaitTime:  opts.WaitTime,
				})

				if err != nil {
					c.logger.Printf("Error watching key %s: %v", key, err)
					time.Sleep(opts.RetryTime)
					continue
				}

				if pair != nil && meta.LastIndex > waitIndex {
					if err := json.Unmarshal(pair.Value, config); err != nil {
						c.logger.Printf("Error parsing config for %s: %v", key, err)
						continue
					}
					c.logger.Printf("Config updated: %s", key)
				}

				waitIndex = meta.LastIndex
			}
		}
	}()

	return nil
}
