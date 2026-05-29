// ============================================================================
// 文件说明：internal/adapter/singbox/grpc_client.go
// 职责概览：实现通过 V2Ray Stats gRPC API 动态拉取 Sing-box 底层精准流量数据的接口客户端。
//           对外实现 GRPCTrafficReader。
//           为了极致的包大小和零编译摩擦阻碍，本组件使用了轻量级“内联二进制 Protobuf 消息编码解码”
//           模式，彻底免除了引入复杂庞大的外部 V2Ray API protobuf 生成库的编译烦恼。
//           通过抓取以 `user>>>` 开头的内部流量键，高内聚的翻译拆解出对应用户的
//           上传、下载流量字节数，作为主程序定时流量轮询最优先、最精准的首选数据源。
// ============================================================================

package singbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"vpnview/internal/port"
)

// statsServiceQueryMethod 是 V2Ray 官方自带的流量监控 gRPC 终点 QueryStats 方法的全网调用路由。
const statsServiceQueryMethod = "/v2ray.core.app.stats.command.StatsService/QueryStats"

// GRPCTrafficReader 利用 gRPC 接口直连 Sing-box 内部抓取最底层的统计数据。
type GRPCTrafficReader struct {
	conn *grpc.ClientConn // 底层维持的 gRPC 物理网络连接句柄
}

// NewGRPCTrafficReader 创建并初始化一个 GRPCTrafficReader 客户端。
// 连接超时设为 5 秒，采用无证书明文（Insecure）传输机制（适配本地回环接口）。
func NewGRPCTrafficReader(address string) (*GRPCTrafficReader, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &GRPCTrafficReader{conn: conn}, nil
}

// QueryTraffic 发起 gRPC 请求，拉取所有以 `user>>>` 前缀匹配的流量统计报表。
// 数据流与精密度：
//  - 这是 Sing-box 原生提供的最精准的累计统计方案（不会发生漏记）。
//  - 获取数据后解析 `user>>>[UserID]>>>traffic>>>uplink|downlink` 指标并汇总返回。
func (r *GRPCTrafficReader) QueryTraffic(ctx context.Context) ([]port.TrafficSnapshot, error) {
	req := &QueryStatsRequest{
		Pattern: "user>>>",
		Reset_:  false, // 仅查询，不重置底层计数器，由主程序差值算速度
	}
	var resp QueryStatsResponse
	// 🏆 巧妙之处：利用 legacyProtoCodec 自定义编解码，完美兼容 Go 较新 grpc 库下编解码 v1 proto 的兼容问题
	if err := r.conn.Invoke(ctx, statsServiceQueryMethod, req, &resp, grpc.ForceCodec(legacyProtoCodec{})); err != nil {
		return nil, err
	}

	byUser := make(map[string]*port.TrafficSnapshot)
	for _, stat := range resp.Stat {
		userID, direction, ok := parseUserTrafficStat(stat.Name)
		if !ok {
			continue
		}
		snap := byUser[userID]
		if snap == nil {
			snap = &port.TrafficSnapshot{UserID: userID}
			byUser[userID] = snap
		}
		if direction == "uplink" {
			snap.Upload += stat.Value
		} else if direction == "downlink" {
			snap.Download += stat.Value
		}
	}

	out := make([]port.TrafficSnapshot, 0, len(byUser))
	for _, snap := range byUser {
		out = append(out, *snap)
	}
	return out, nil
}

// Close 关闭与 Sing-box API 端的物理 gRPC 长连接。
func (r *GRPCTrafficReader) Close() error {
	if r.conn == nil {
		return nil
	}
	return r.conn.Close()
}

// parseUserTrafficStat 解构翻译 V2Ray 原生监控的指标名称字符串。
// 格式："user>>>[UserID]>>>traffic>>>uplink|downlink"
// 返回解出的用户 ID、传输方向、及合法性 ok 标志。
func parseUserTrafficStat(name string) (userID, direction string, ok bool) {
	parts := strings.Split(name, ">>>")
	if len(parts) < 4 || parts[0] != "user" || parts[2] != "traffic" {
		return "", "", false
	}
	if parts[3] != "uplink" && parts[3] != "downlink" {
		return "", "", false
	}
	return parts[1], parts[3], true
}

// legacyProtoCodec 专用轻量编解码驱动。
// 解决较新版本的 gRPC-go 默认仅期望 v2 格式 proto 消息的阻碍，维持内联 v1 消息的安全运行。
type legacyProtoCodec struct{}

func (legacyProtoCodec) Marshal(v any) ([]byte, error) {
	msg, ok := v.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("legacy proto 编解码器期望传入 proto.Message, 实际传入 %T", v)
	}
	return proto.Marshal(msg)
}

func (legacyProtoCodec) Unmarshal(data []byte, v any) error {
	msg, ok := v.(proto.Message)
	if !ok {
		return fmt.Errorf("legacy proto 编解码器期望传入 proto.Message, 实际传入 %T", v)
	}
	return proto.Unmarshal(data, msg)
}

func (legacyProtoCodec) Name() string {
	return "proto"
}

// ============================================================================
// 下方为内联的轻量级 Protobuf 消息结构体，免除安装外部 protoc 工具生成的编译壁垒。
// ============================================================================

// QueryStatsRequest V2Ray 接口请求结构。
type QueryStatsRequest struct {
	Pattern string `protobuf:"bytes,1,opt,name=pattern,proto3" json:"pattern,omitempty"` // 指标键名过滤正则
	Reset_  bool   `protobuf:"varint,2,opt,name=reset,proto3" json:"reset,omitempty"`    // 查询完毕后是否物理清零底座计数器
}

func (m *QueryStatsRequest) ResetMessage() { *m = QueryStatsRequest{} }
func (m *QueryStatsRequest) String() string {
	return proto.CompactTextString(m)
}
func (*QueryStatsRequest) ProtoMessage() {}
func (m *QueryStatsRequest) Reset()      { m.ResetMessage() }

// QueryStatsResponse V2Ray 接口响应结构。
type QueryStatsResponse struct {
	Stat []*Stat `protobuf:"bytes,1,rep,name=stat,proto3" json:"stat,omitempty"` // 扫描到的指标详情列表
}

func (m *QueryStatsResponse) Reset()         { *m = QueryStatsResponse{} }
func (m *QueryStatsResponse) String() string { return proto.CompactTextString(m) }
func (*QueryStatsResponse) ProtoMessage()    {}

// Stat 单个统计指标名与其累计数值（字节数）。
type Stat struct {
	Name  string `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`    // 指标名字，如 "user>>>demo>>>traffic>>>uplink"
	Value int64  `protobuf:"varint,2,opt,name=value,proto3" json:"value,omitempty"` // 指标总数值（累计字节数）
}

func (m *Stat) Reset()         { *m = Stat{} }
func (m *Stat) String() string { return proto.CompactTextString(m) }
func (*Stat) ProtoMessage()    {}
