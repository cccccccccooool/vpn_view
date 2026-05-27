// grpc_client.go 实现了通过 V2Ray Stats gRPC API 读取用户流量统计数据的功能。
// 使用轻量级的内联 protobuf 消息定义，无需引入完整的 V2Ray proto 依赖。
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

// statsServiceQueryMethod 是 V2Ray Stats gRPC 服务的 QueryStats 方法全路径。
const statsServiceQueryMethod = "/v2ray.core.app.stats.command.StatsService/QueryStats"

// GRPCTrafficReader 通过 V2Ray Stats gRPC API 查询用户流量统计数据。
// 实现了 TrafficReader 接口。
type GRPCTrafficReader struct {
	conn *grpc.ClientConn // 底层 gRPC 连接
}

// NewGRPCTrafficReader 创建一个新的 GRPCTrafficReader，连接到指定的 V2Ray Stats gRPC 地址。
// 使用 insecure 凭证（不加密），适用于本地回环地址场景。
func NewGRPCTrafficReader(address string) (*GRPCTrafficReader, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &GRPCTrafficReader{conn: conn}, nil
}

// QueryTraffic 查询所有用户的累计流量统计。
// 通过匹配 "user>>>" 前缀获取用户级别的上行/下行流量数据。
func (r *GRPCTrafficReader) QueryTraffic(ctx context.Context) ([]port.TrafficSnapshot, error) {
	req := &QueryStatsRequest{
		Pattern: "user>>>",
		Reset_:  false,
	}
	var resp QueryStatsResponse
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

// Close 关闭底层的 gRPC 连接。
func (r *GRPCTrafficReader) Close() error {
	if r.conn == nil {
		return nil
	}
	return r.conn.Close()
}

// parseUserTrafficStat 解析 V2Ray 统计项名称（格式: "user>>>userID>>>traffic>>>uplink|downlink"）。
// 返回解析出的用户 ID、方向（uplink/downlink）以及是否解析成功。
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

// legacyProtoCodec keeps the inline v1 protobuf messages compatible with
// newer grpc-go versions, whose default codec expects v2 protobuf messages.
type legacyProtoCodec struct{}

func (legacyProtoCodec) Marshal(v any) ([]byte, error) {
	msg, ok := v.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("legacy proto codec expects proto.Message, got %T", v)
	}
	return proto.Marshal(msg)
}

func (legacyProtoCodec) Unmarshal(data []byte, v any) error {
	msg, ok := v.(proto.Message)
	if !ok {
		return fmt.Errorf("legacy proto codec expects proto.Message, got %T", v)
	}
	return proto.Unmarshal(data, msg)
}

func (legacyProtoCodec) Name() string {
	return "proto"
}

// QueryStatsRequest 是 V2Ray StatsService.QueryStats 的请求消息（内联 protobuf 定义）。
type QueryStatsRequest struct {
	Pattern string `protobuf:"bytes,1,opt,name=pattern,proto3" json:"pattern,omitempty"` // 统计项名称匹配模式
	Reset_  bool   `protobuf:"varint,2,opt,name=reset,proto3" json:"reset,omitempty"`    // 查询后是否重置计数器
}

func (m *QueryStatsRequest) ResetMessage() { *m = QueryStatsRequest{} }
func (m *QueryStatsRequest) String() string {
	return proto.CompactTextString(m)
}
func (*QueryStatsRequest) ProtoMessage() {}
func (m *QueryStatsRequest) Reset()      { m.ResetMessage() }

// QueryStatsResponse 是 V2Ray StatsService.QueryStats 的响应消息（内联 protobuf 定义）。
type QueryStatsResponse struct {
	Stat []*Stat `protobuf:"bytes,1,rep,name=stat,proto3" json:"stat,omitempty"` // 匹配的统计项列表
}

func (m *QueryStatsResponse) Reset()         { *m = QueryStatsResponse{} }
func (m *QueryStatsResponse) String() string { return proto.CompactTextString(m) }
func (*QueryStatsResponse) ProtoMessage()    {}

// Stat 表示单个统计项，包含名称和数值（内联 protobuf 定义）。
type Stat struct {
	Name  string `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`    // 统计项名称，如 "user>>>alice>>>traffic>>>uplink"
	Value int64  `protobuf:"varint,2,opt,name=value,proto3" json:"value,omitempty"` // 统计值（字节数）
}

func (m *Stat) Reset()         { *m = Stat{} }
func (m *Stat) String() string { return proto.CompactTextString(m) }
func (*Stat) ProtoMessage()    {}
