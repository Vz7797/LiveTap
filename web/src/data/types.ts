import type { MsgType, RoomStatus, Source, Tone } from "@/lib/meta";

export type Counts = Record<MsgType, number>;

export interface RoomEvent {
  at: number;
  kind: "ready" | "reconnect" | "handshake_fail" | "risk" | "offline" | "online" | "rebuild" | "probe";
  text: string;
}

/** 进入推送前被丢弃的上游消息：按原因（超大 / 队列满）和消息类型分项 */
export interface DropStats {
  total: number;
  /** 最近一次丢弃的时间（Unix 毫秒），没有丢弃时为 0 */
  lastAt: number;
  /** 入口队列积压过的最高条数和字节数，用来判断丢弃是突发涌入还是处理被卡住 */
  peakItems: number;
  peakBytes: number;
  reason: { oversize: number; full: number };
  type: { chat: number; gift: number; social: number; member: number; other: number };
}

export interface Room {
  roomId?: string;
  reason?: string;
  /** 机器可读的原因码，取值见 docs/api.md */
  reasonCode?: string;
  noData?: boolean;
  drops?: DropStats;
  options?: import("@/components/collector-options").PublicOptions;
  overrides?: string[];
  liveId: string; // web_rid
  name: string;
  title: string;
  city: string;
  category: string;
  status: RoomStatus;
  source: Source;
  tags: string[];
  engine: "douyinlive" | "browser";
  sign: "local" | "tikhub";
  protocol: "web" | "pc";
  proxyId: string | null;
  accountId: string | null;
  online: number;
  totalPv: number;
  /** 热度：每秒模拟消息数基准 */
  heat: number;
  msgPerMin: number;
  reconnects30m: number;
  lastMsgAt: number;
  since: number;
  /** 最近 30 分钟每分钟消息条数（旧到新） */
  spark: number[];
  /** 最近 30 分钟每分钟在线人数（旧到新，断流超过 3 分钟记 0） */
  onlineSpark: number[];
  counts: Counts;
  subscribers: number;
  /** 订阅这个房间的接入方名称（去重） */
  subscriberClients?: string[];
  /** 订阅明细：接入方与业务标识（如业务系统的商家/场次），仅详情接口返回 */
  subscriptions?: { client: string; externalRef: string }[];
  /** 主播头像缩略图地址 */
  avatar?: string;
  /** 自动停止检测的时间与原因：live_ended 已下播 / never_live 长时间未开播 */
  endedAt?: number;
  endedReason?: string;
  /** 最近一次开始采集（开播）的时间 */
  lastLiveAt?: number;
  silenceThreshold: number;
  events: RoomEvent[];
  enabledTypes: MsgType[];
  /** 每分钟统计（最近 30 分钟） */
  minutes: Counts[];
  note?: string;
}

export interface FeedMsg {
  id: string;
  seq: number;
  type: MsgType;
  liveId: string;
  roomName: string;
  nickname: string;
  /** 观众头像缩略图地址 */
  avatar?: string;
  content: string;
  extra?: { gift?: string; count?: number; action?: "关注" | "分享"; via?: string };
  ts: number;
}

export interface Proxy {
  id: string;
  name: string;
  url: string; // 脱敏后的地址
  protocol: "HTTP" | "SOCKS5";
  region: string;
  isp: string;
  enabled: boolean;
  health: "ok" | "slow" | "down" | "unknown";
  latency: number | null;
  history: number[];
  rooms: number;
  lastTest: number;
}

export interface Account {
  id: string;
  note: string;
  cookieMask: string;
  validity: "valid" | "expired" | "limited" | "checking";
  rooms: string[];
  limit: number;
  lastCheck: number;
  addedAt: number;
}

export interface Client {
  id: string;
  name: string;
  env: "生产" | "预发" | "测试";
  keyPrefix: string;
  createdAt: number;
  lastSeen: number;
  enabled: boolean;
  quota: number;
  used: number;
  whitelist: string[];
  connections: number;
  pushPerSec: number;
  bytesPerSec?: number;
  dropped: number;
  queue: number;
  spark: number[];
}

export interface Incident {
  id: string;
  at: number;
  level: "critical" | "warning" | "notice";
  title: string;
  detail: string;
  liveId?: string;
  acked: boolean;
}

export interface TimelineItem {
  id: string;
  at: number;
  tone: Tone;
  title: string;
  detail: string;
}

export interface LogLine {
  id: number;
  at: number;
  level: "DEBUG" | "INFO" | "WARN" | "ERROR";
  liveId?: string;
  msg: string;
  fields?: string;
}

/** 最近 1 分钟的延迟分位数；precise=false 表示上游时间只精确到秒（已按半秒居中） */
export interface LagStats { p50_ms: number; p95_ms: number; samples: number; precise: boolean }

export interface EngineStatus {
  enabled: boolean;
  repo: string;
  version: string;
  latest: string;
  latestUrl: string;
  publishedAt: number;
  checkedAt: number;
  newer: boolean;
  error?: string;
}

export interface NodeStats {
  /** upstream：抖音发出 → 本节点收到；push：本节点收到 → 推给接入方 */
  latency?: { upstream?: LagStats; push?: LagStats };
  /** 节点启动以来所有直播间的入口丢弃（含已删除的直播间） */
  entryDrops?: DropStats;
  northbound?: { connections: number; queue: number; dropped: number; push_per_sec: number; bytes_per_sec: number; sent: number; bytes: number };
	 cooldown?: { active: boolean; until: number; remainingSeconds: number; strikes: number };
	 homepageRequestsPerMinute?: number;
	 shareGuestIdentity?: boolean;
  bandwidthAvailable?: boolean;
  reqHist?: number[];
  name: string;
  version: string;
  /** 构建所用提交，前 12 位；本地未提交改动时带 -dirty */
  revision?: string;
  engineVersion: string;
  /** 采集内核与作者在 GitHub 上的最新版本；节点每 6 小时查一次，LIVETAP_UPDATE_CHECK=false 时不查 */
  engine?: EngineStatus;
  bootId: string;
  startedAt: number;
  cpu: number;
  memMB: number;
  memLimitMB: number;
  goroutines: number;
  upKBs: number;
  downKBs: number;
  conns: number;
  reqPerMin: number;
  reqBudgetPerSec: number;
  maxRooms: number;
  health: number;
  cpuHist: number[];
  memHist: number[];
  /** 下行 / 上行带宽最近 72 个采样（KB/s），容器自身网卡的收发量 */
  netHist: number[];
  upHist?: number[];
}
