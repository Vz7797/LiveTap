// 模拟数据用的中文语料：昵称、弹幕、礼物等。全部为虚构内容。

export const NICKNAMES = [
  "爱吃火锅的猫", "小王同学", "江南烟雨", "阿杰不加班", "Lily🌸", "用户8392014", "奶茶续命中",
  "周末去哪吃", "大胃王小美", "一只柠檬精", "晚风", "吃货研究所", "momo", "成都老饕", "Kevin",
  "今天也要开心", "胖虎", "鱼香肉丝盖饭", "不吃香菜", "夏天的风", "王阿姨", "七七", "阿峰",
  "小鹿乱撞", "干饭人", "吃不胖的Amy", "南山南", "用户2219087", "隔壁老李", "甜甜圈",
  "西瓜太郎", "橘子汽水", "Leo", "早睡早起", "椰子", "团购达人", "探店小分队", "螺蛳粉爱好者",
  "芒果布丁", "深夜食堂", "张大厨", "吃饱了再减肥", "Rainy", "小太阳", "一颗卤蛋", "路过的猫",
];

export const CHATS = [
  "这个套餐几点能用？", "周末可以用吗", "有停车位吗？", "主播再讲一下双人餐", "已拍，期待！",
  "多少钱一份", "链接在哪里", "能开发票吗", "好吃吗？真实评价", "怎么核销呀", "限购几份？",
  "666", "上车上车", "求返场！", "节假日通用吗", "需要提前预约吗", "包间要另外收费吗",
  "小朋友几岁以下免费", "可以打包带走吗", "离地铁站远不远", "第一次来，求推荐", "已下单两份",
  "还有库存吗", "上次去吃过，味道不错", "主播好可爱", "锅底可以换吗", "是全国通用吗",
  "这个价格太香了", "能叠加优惠吗", "过期能退吗", "晚上几点关门", "收藏了", "家人们冲",
  "有没有素食", "辣度能调吗", "一个人能吃完吗", "再来一单", "下次直播什么时候", "点赞了",
  "已关注，等返场", "在成都哪个区", "环境怎么样", "适合聚餐吗", "排队要多久",
];

export const GIFTS = [
  { name: "小心心", value: 1 },
  { name: "玫瑰", value: 1 },
  { name: "人气票", value: 1 },
  { name: "粉丝灯牌", value: 10 },
  { name: "加油鸭", value: 15 },
  { name: "大啤酒", value: 2 },
  { name: "鲜花", value: 10 },
  { name: "墨镜", value: 199 },
  { name: "抖音1号", value: 2999 },
];

export const ENTER_SOURCES = ["推荐页", "同城", "搜索", "关注页", "分享链接", "直播广场"];

export function pick<T>(arr: readonly T[]): T {
  return arr[Math.floor(Math.random() * arr.length)];
}

export function weighted<T extends string>(weights: Record<T, number>): T {
  const entries = Object.entries(weights) as [T, number][];
  const total = entries.reduce((s, [, w]) => s + w, 0);
  let r = Math.random() * total;
  for (const [k, w] of entries) {
    r -= w;
    if (r <= 0) return k;
  }
  return entries[0][0];
}

/** 近似正态的小扰动 */
export function jitter(base: number, ratio = 0.08) {
  const n = (Math.random() + Math.random() + Math.random() - 1.5) / 1.5;
  return base * (1 + n * ratio);
}

export function randId(len = 19) {
  let s = String(Math.floor(Math.random() * 9) + 1);
  while (s.length < len) s += Math.floor(Math.random() * 10);
  return s;
}
