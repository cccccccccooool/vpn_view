// ============================================================================
// 文件说明：internal/identity/resolver.go
// 职责概览：实现 VPN 网络连接与用户身份映射的解析器（Resolver）。
//           网络代理底层捕获到的活动连接只带有基础字段（IP、来源标记、RouteTag），
//           此模块负责根据元数据键、特定的分流路由标签还原函数、以及 IP 反查解析函数，
//           按照预定优先级（元数据解析 -> 路由标签解码 -> IP 反查兜底），
//           将杂乱的底层网络活动连接归类并精确定位到系统管理的具体用户 ID 上。
// ============================================================================

package identity

import (
	"strings"
)

// HintSource 描述解析器提取到的用户身份特征线索（Hint）的数据源来源类型。
type HintSource string

const (
	HintMetadata HintSource = "metadata" // 来源于底层连接协议握手时自带的元数据（如用户名 inbound_user）
	HintChain    HintSource = "chain"    // 来源于底层特定分流路由标记规则链（如分流 Tag）
	HintIP       HintSource = "ip"       // 来源于网络包客户端连接来源 IP 地址
)

// Hint 封装了底层 VPN 监测到的单一用户身份印记/线索。
type Hint struct {
	Source HintSource // 线索来源类型
	Key    string     // 属性 Key 标识
	Value  string     // 对应的值（常包含用户 ID、连接 IP 等信息）
}

// RouteTagDecoder 用户定义的分流路由匹配标记 Tag 转换并解码还原为用户 ID 的辅助函数指针。
type RouteTagDecoder func(tag string) string

// IPResolver 从 IPMap 内存中，输入连接来源 IP 地址反向求出用户 ID 的辅助函数指针（最后方案）。
type IPResolver func(ip string) string

// Resolver 调度整合器。按照高内聚优先级原则，解析外部传递的多维度线索，提取出精密的 VPN 用户 ID。
type Resolver struct {
	MetadataKeys    []string        // 明确指出并被允许信任的元数据 Key 列表（如 "user_id", "inbound_user"）
	RouteTagDecoder RouteTagDecoder // 绑定的分流路由标签解码工具
	IPResolver      IPResolver      // 绑定的 IP 反查工具
	AllowIPFallback bool            // 是否支持当协议无任何数据时，允许通过客户端 IP 进行反查对齐
}

// Resolve 解析调度主方法。按照以下优先级次序筛选并返回高可信度的用户 ID：
//  1. 检索 hints 中类型为 HintMetadata 的条目，若其 Key 在 MetadataKeys 授信列表中，直接返回 Value。
//  2. 检索 HintChain 分流路由标签条目，调用 RouteTagDecoder 进行解密翻译。
//  3. 若开启 AllowIPFallback 且无结果，检索 HintIP IP 映射条目，调用 IPResolver 内存反查对齐。
//  若以上皆无结果，返回空字符串 "" 代表无法确定连接归属。
func (r Resolver) Resolve(hints []Hint) string {
	// 阶段一：元数据优先精确匹配
	for _, hint := range hints {
		if hint.Source != HintMetadata || strings.TrimSpace(hint.Value) == "" {
			continue
		}
		if r.acceptMetadataKey(hint.Key) {
			return strings.TrimSpace(hint.Value)
		}
	}

	// 阶段二：分流链标记 RouteTag 解析
	if r.RouteTagDecoder != nil {
		for _, hint := range hints {
			if hint.Source != HintChain || strings.TrimSpace(hint.Value) == "" {
				continue
			}
			if userID := r.RouteTagDecoder(strings.TrimSpace(hint.Value)); userID != "" {
				return userID
			}
		}
	}

	// 阶段三：降级 IP 绑定关系反查
	if r.AllowIPFallback && r.IPResolver != nil {
		for _, hint := range hints {
			if hint.Source != HintIP || strings.TrimSpace(hint.Value) == "" {
				continue
			}
			if userID := r.IPResolver(strings.TrimSpace(hint.Value)); userID != "" {
				return userID
			}
		}
	}

	return "" // 无法辨别连接所有人
}

// acceptMetadataKey 检测给定的元数据 Key 是否属于被 Resolver 信任的属性集合范围内。
func (r Resolver) acceptMetadataKey(key string) bool {
	if len(r.MetadataKeys) == 0 {
		return true // 如果未定义 MetadataKeys，默认全部信任放行
	}
	for _, allowed := range r.MetadataKeys {
		if key == allowed {
			return true
		}
	}
	return false
}
