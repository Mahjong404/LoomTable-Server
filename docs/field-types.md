# Field Type 规范

## Field Type Interface

每个 Field Type 都必须定义自己的值语义和交互行为：

```ts
interface FieldTypeDefinition<TValue, TConfig> {
  type: string;
  validate(value: unknown, config: TConfig): ValidationResult;
  normalize(value: unknown, config: TConfig): TValue | null;
  format(value: TValue, config: TConfig): string;
  render(value: TValue, context: RenderContext): HTMLElement;
  createEditor(context: EditorContext): EditorHandle;
  serialize(value: TValue): unknown;
  deserialize(value: unknown): TValue | null;
  getQueryOperators?(config: TConfig): QueryOperator[];
}
```

Field Type 是领域行为，不只是一个 UI 控件。服务端和 Plugin 必须对值的合法性、标准化和序列化保持一致。

P0 的九种 Field 都允许显式 `null`，表示该 Cell 已被明确清空；`Record.values` 中缺少 Field ID 则表示 Unset Cell。Text/LongText 的 `""` 与 MultiSelect 的 `[]` 是合法且与 `null`、Unset 不同的自然空值。空 URL 字符串和不含任何有效成员的 Location 对象无有效语义，返回 `422 VALIDATION_ERROR`，不得静默规范化成其他状态。

Filter 的 `isEmpty` 同时匹配 Unset、`null` 和该类型的自然空值；`isNotEmpty` 是其反集。这只定义查询集合，不合并三种存储状态。

P0 值限制：Text 最多 10,000、LongText 最多 100,000 个 Unicode 码点；URL 最多 2,048 个字符且必须是绝对 HTTP/HTTPS URL；Number 必须是有限 IEEE-754 数；Date 必须是有效 Gregorian `YYYY-MM-DD`；MultiSelect 最多包含 100 个互异 Option ID；Location 的 `label`、`address`、`provider` 分别最多 500、2,000、100 个 Unicode 码点。

## 第一阶段字段

| Type ID | 中文 | 值形态 | 第一阶段状态 |
|---|---|---|---|
| `text` | 文本 | string 或 null | P0 |
| `longText` | 长文本 | string 或 null | P0 |
| `number` | 数字 | number 或 null | P0 |
| `checkbox` | 勾选 | boolean 或 null | P0 |
| `date` | 日期 | 标准化日期值或 null | P0 |
| `select` | 单选 | option ID 或 null | P0 |
| `multiSelect` | 多选 | option ID 数组或 null | P0 |
| `url` | URL | URL 字符串或 null | P0 |
| `location` | 地点 | LocationValue 或 null | P0 |
| `attachment` | 附件 | AttachmentRef 数组 | P1 |
| `relation` | 关联 | Record ID 数组 | P1 |
| `email` | 邮箱 | email 字符串 | P1 |
| `noteLink` | 笔记链接 | Vault 相对路径或链接值 | P1 |

## 后续字段和查询能力

以下能力已纳入领域模型，但不属于 P0。它们必须通过新的 Field Type 或 Query Operator 正式加入，不把复杂语义临时塞进现有字段：

### Region

`region` 是独立的字段类型，不是 `Location` 的配置项，也不是带层级的 `select`。Server 管理区域目录，允许不同国家使用不同层级；值至少包含稳定的 Region Code、目录版本和可显示的层级路径：

~~~json
{
  "code": "CN-31-3101",
  "catalogVersion": "2026-01",
  "path": [
    {"level": "country", "code": "CN", "name": "中国"},
    {"level": "province", "code": "CN-31", "name": "上海市"},
    {"level": "city", "code": "CN-31-3101", "name": "上海市"}
  ]
}
~~~

区域目录的显示名称可以本地化，但稳定 Code 不随翻译变化。目录升级必须保留原目录版本，并提供明确的迁移策略。

### DateTime 和 Time

P0 的 `date` 继续表示 `YYYY-MM-DD` 的纯日期。后续增加：

- `dateTime`：单值或时间范围；单值使用 UTC RFC3339，范围使用 `start`/`end`。
- `time`：不带日期的本地时刻，单值或时间范围，规范值为 `HH:mm[:ss]`。

三者的 Range 模式都必须明确 `start <= end`，并在 Query 中区分空值、单值和范围重叠语义。P0 不实现这些类型。

### Location 范围检索

后续为 `location` 增加 `geoWithin` 查询操作，先支持矩形和圆形，Polygon 后置。只有同时具有合法纬度和经度的 Location 才能匹配；范围筛选可以保存到 View Filter。没有坐标的 Location 不应被隐式当作位于范围外或某个默认点。

### 格式和校验预设

- `number` 始终表示真正的数值；Currency、Percent 属于 Number 的显示/格式配置。
- 手机号、身份证号等不是 Number 的特殊数值组合，而是 `text` 的区域化校验预设，例如 `phone`、`nationalId`。
- Plugin 和 Server 必须共享这些预设的规范、区域参数、校验和标准化规则。
- 任意用户 Regex 不属于 P0；若未来开放，必须明确安全限制和跨端一致性。
- Rating、Duration、User 等具有独立领域语义的能力，未来再作为独立 Field Type 评估。

## 计算字段

| Type ID | 中文 | 语义 | 计划 |
|---|---|---|---|
| `formula` | 公式 | 根据当前 Record 计算 | P2 |
| `lookup` | 查找 | 读取关联 Record 的字段 | P2 |
| `rollup` | 汇总 | 聚合关联 Record 的值 | P2 |

计算字段是只读的。计算结果由 Server 统一产生，Plugin 不提交计算结果作为普通 Mutation。

## Location

```ts
type LocationValue = {
  label?: string;
  address?: string;
  lat?: number;
  lng?: number;
  provider?: string;
  precision?: "exact" | "rooftop" | "approximate";
};
```

规则：

- `lat` 范围为 `-90..90`。
- `lng` 范围为 `-180..180`。
- P0 的 `lat` 和 `lng` 使用 WGS 84 经纬度语义；Server 和 Plugin 都不得隐式转换为 GCJ-02、BD-09 或其他坐标系。
- `lat` 和 `lng` 必须同时存在才可在 Map View 显示 Marker。
- `label`、`address`、`provider` 执行 Unicode Trim、NFC 和控制字符拒绝；规范化为空的成员被省略。省略后 Location 至少保留 `label`、`address`、`provider` 或完整坐标对之一，不能只保存 `precision`。
- 有效 WGS 84 坐标超出 EPSG:3857 的可渲染纬度 `±85.0511287798066` 时仍原样保存，但 Map View 不创建 Marker，并将其计入不可渲染数量而不是未定位数量。
- `label` 或 `address` 可以在没有坐标时独立存在。
- 用户可以手动输入文本、输入坐标或在地图上选点。
- 地理编码是可选 Adapter，不是核心前置依赖。
- Map View 对没有坐标或坐标无效的 Location 显示未定位数量，不创建虚假坐标。

## Attachment

```ts
type AttachmentRef = {
  id: string;
  source: "managed" | "vault";
  filename: string;
  mimeType?: string;
  size?: number;
  storageKey?: string;
  vaultPath?: string;
  hash?: string;
  width?: number;
  height?: number;
};
```

规则：

- 一个 Attachment Cell 使用数组值。
- Field Config 为 `{maxCount}`，缺省为 10，范围为 1–100；单文件字段使用 `maxCount: 1`。
- `managed` 使用 Server 文件存储。
- `vault` 使用 Vault 相对路径。
- Managed Attachment 必须先完成内容上传并进入 `ready` 状态，Record 才能引用。
- Record Mutation 校验 Attachment ID、source、当前 Actor 所有权和未删除状态；未知、未完成或其他 Actor 的引用返回 `422 VALIDATION_ERROR`。
- Vault Attachment 不承诺跨设备可用。
- 图片优先提供缩略图和懒加载预览。
- Attachment 删除先解除引用，物理文件清理由保留策略处理。

## Select、MultiSelect 与 Tag

Tag 不是独立 Field Type：

- 一个标签使用 `select`。
- 多个标签使用 `multiSelect`。
- Select/MultiSelect Option 使用 Server 生成且永不复用的 `opt_...` ID；Record 值引用 ID，不引用可变名称。
- Option 配置输入是期望的 Active 列表：无 ID 创建，当前 Active ID 更新，Deleted ID 恢复，遗漏 Active ID 软删除；响应分别返回 Active 与 Deleted Option，Active 数组顺序即显示顺序。
- Option 名称最多 100 个 Unicode 码点并执行 Unicode Trim、NFC 和控制字符拒绝；Active 名称按 locale-neutral Unicode Default Case Folding 后唯一，Deleted Option 可以与 Active Option 同名。
- 每个 Field 最多 500 个 Active Option、5,000 个 Active + Deleted Option；Deleted Option 按 `deletedAt ASC, id ASC` 返回。
- Option `color` 是必填的 Server 语义色板 Token，只允许 `gray`、`red`、`orange`、`yellow`、`green`、`cyan`、`blue`、`purple`、`pink`，不接受 CSS 颜色值。
- Chip、颜色和标签样式属于 Renderer。
- 跨 Table 共享标签、标签层级和标签权限属于未来 Tag Domain，不属于第一阶段。

## Field Schema 变更

- Field ID 永久稳定。
- 改名不改变 Field ID 和 Cell 值。
- 删除先进入回收站或标记为 deleted。
- P0 不允许类型变更；只允许改名和不改变值语义的 Config 完整替换。Text、LongText、Number、Checkbox、Date、URL、Location 的 Config 固定为 `{}`；Select/MultiSelect 只接受 Option 生命周期配置。
- Field、CreateFieldRequest 和 UpdateFieldRequest 使用 `type` 判别的 P0 Field 联合类型；更新请求必须回显不可变的 `type`。每种 Config 都拒绝未声明属性，不能让任意 JSON 穿透到领域层。
- `Field.schemaVersion` 标识服务端规范化配置的 Schema 版本；版本不嵌套在 Config 对象中。
- 后续开放类型变更时，必须先提供迁移预览，并把无法转换的值放入明确的迁移错误列表。
- 未识别的未来 Field Type 不得被静默转换为 Text。

