// ============================================================================
// 文件说明：internal/adapter/xray/grpc_client.go
// 职责概览：实现通过 Xray / V2Ray 的 gRPC Stats API 动态拉取底层精准用户流量数据的客户端。
//           对外实现 TrafficReader。为了避免引入庞大的官方 protobuf 生成库（Xray 与 V2Ray
//           的生成代码互不兼容且体积巨大），本组件采用轻量级 "内联 Protobuf 消息 + 自定义
//           legacy 编解码器" 模式，仅手写 QueryStats 所需的最小消息，即可零摩擦调用两类核心。
//           QueryStats 方法的调用路径由 Config.Variant 决定（xray.app.* 或 v2ray.core.app.*），
//           流量键名遵循两核心一致的 `user>>>[email]>>>traffic>>>uplink|downlink` 规范。
// ============================================================================

package xray

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

// GRPCTrafficReader 利用 gRPC 接口直连 Xray / V2Ray 内置 Stats 服务抓取最底层的统计数据。
type GRPCTrafficReader struct {
	conn        *grpc.ClientConn // 底层维持的 gRPC 物理网络连接句柄
	queryMethod string           // QueryStats 方法的全限定调用路径（按核心变体区分）
}

// NewGRPCTrafficReader 创建并初始化一个面向指定核心变体的 GRPCTrafficReader 客户端。
// 连接超时设为 5 秒，采用无证书明文（Insecure）传输机制（适配本地回环 API 接口）。
func NewGRPCTrafficReader(address, queryMethod string) (*GRPCTrafficReader, error) {
	if queryMethod == "" {
		queryMethod = xrayStatsQueryMethod
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &GRPCTrafficReader{conn: conn, queryMethod: queryMethod}, nil
}

// QueryTraffic 发起 gRPC 请求，拉取所有以 `user>>>` 前缀匹配的用户流量统计报表。
//   - 仅查询不重置底层计数器（Reset=false），由主程序前后两次差值算得实时速度。
//   - 解析 `user>>>[email]>>>traffic>>>uplink|downlink` 键并按用户汇总上下行字节数。
func (r *GRPCTrafficReader) QueryTraffic(ctx context.Context) ([]port.TrafficSnapshot, error) {
	req := &queryStatsRequest{
		Pattern: "user>>>",
		Reset_:  false,
	}
	var resp queryStatsResponse
	// 使用自定义 legacyProtoCodec，兼容较新 grpc-go 库对 v1 proto 消息的编解码要求。
	if err := r.conn.Invoke(ctx, r.queryMethod, req, &resp, grpc.ForceCodec(legacyProtoCodec{})); err != nil {
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
		switch direction {
		case "uplink":
			snap.Upload += stat.Value
		case "downlink":
			snap.Download += stat.Value
		}
	}

	out := make([]port.TrafficSnapshot, 0, len(byUser))
	for _, snap := range byUser {
		out = append(out, *snap)
	}
	return out, nil
}

// Close 关闭与核心 API 端的物理 gRPC 长连接。
func (r *GRPCTrafficReader) Close() error {
	if r.conn == nil {
		return nil
	}
	return r.conn.Close()
}

// parseUserTrafficStat 解构翻译 Xray / V2Ray 原生监控的指标名称字符串。
// 格式："user>>>[email]>>>traffic>>>uplink|downlink"，返回用户 ID（email）、传输方向及合法性标志。
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

// legacyProtoCodec 专用轻量编解码驱动，解决较新 grpc-go 默认仅接受 v2 proto 消息的阻碍。
type legacyProtoCodec struct{}

func (legacyProtoCodec) Marshal(v any) ([]byte, error) {
	msg, ok := v.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("legacy proto 编解码器期望 proto.Message，实际传入 %T", v)
	}
	return proto.Marshal(msg)
}

func (legacyProtoCodec) Unmarshal(data []byte, v any) error {
	msg, ok := v.(proto.Message)
	if !ok {
		return fmt.Errorf("legacy proto 编解码器期望 proto.Message，实际传入 %T", v)
	}
	return proto.Unmarshal(data, msg)
}

func (legacyProtoCodec) Name() string {
	return "proto"
}

// ============================================================================
// 下方为内联的轻量级 Protobuf 消息结构体，与 Xray / V2Ray 的 StatsService QueryStats
// 请求/响应线格式（wire format）一致，免除安装外部 protoc 工具生成的编译壁垒。
// ============================================================================

// queryStatsRequest 对应 StatsService.QueryStats 的请求消息。
type queryStatsRequest struct {
	Pattern string `protobuf:"bytes,1,opt,name=pattern,proto3" json:"pattern,omitempty"` // 指标键名子串过滤
	Reset_  bool   `protobuf:"varint,2,opt,name=reset,proto3" json:"reset,omitempty"`    // 查询后是否物理清零计数器
}

func (m *queryStatsRequest) Reset()         { *m = queryStatsRequest{} }
func (m *queryStatsRequest) String() string { return proto.CompactTextString(m) }
func (*queryStatsRequest) ProtoMessage()    {}

// queryStatsResponse 对应 StatsService.QueryStats 的响应消息。
type queryStatsResponse struct {
	Stat []*stat `protobuf:"bytes,1,rep,name=stat,proto3" json:"stat,omitempty"` // 扫描命中的指标详情列表
}

func (m *queryStatsResponse) Reset()         { *m = queryStatsResponse{} }
func (m *queryStatsResponse) String() string { return proto.CompactTextString(m) }
func (*queryStatsResponse) ProtoMessage()    {}

// stat 单个统计指标的键名与其累计数值（字节数）。
type stat struct {
	Name  string `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`    // 指标名，如 "user>>>alice>>>traffic>>>uplink"
	Value int64  `protobuf:"varint,2,opt,name=value,proto3" json:"value,omitempty"` // 累计字节数
}

func (m *stat) Reset()         { *m = stat{} }
func (m *stat) String() string { return proto.CompactTextString(m) }
func (*stat) ProtoMessage()    {}
