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

## 第一阶段字段

| Type ID | 中文 | 值形态 | 第一阶段状态 |
|---|---|---|---|
| `text` | 文本 | string | P0 |
| `longText` | 长文本 | string | P0 |
| `number` | 数字 | number 或 null | P0 |
| `checkbox` | 勾选 | boolean | P0 |
| `date` | 日期 | 标准化日期值 | P0 |
| `select` | 单选 | option ID 或 null | P0 |
| `multiSelect` | 多选 | option ID 数组 | P0 |
| `url` | URL | URL 字符串 | P0 |
| `location` | 地点 | LocationValue | P0 |
| `attachment` | 附件 | AttachmentRef 数组 | P1 |
| `relation` | 关联 | Record ID 数组 | P1 |
| `email` | 邮箱 | email 字符串 | P1 |
| `noteLink` | 笔记链接 | Vault 相对路径或链接值 | P1 |

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
- `lat` 和 `lng` 必须同时存在才可在 Map View 显示 Marker。
- `label` 或 `address` 可以在没有坐标时独立存在。
- 用户可以手动输入文本、输入坐标或在地图上选点。
- 地理编码是可选 Adapter，不是核心前置依赖。
- Map View 对没有坐标的 Location 显示未定位数量，不创建虚假坐标。

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
- Field Config 可以设置 `maxCount`；单文件字段使用 `maxCount: 1`。
- `managed` 使用 Server 文件存储。
- `vault` 使用 Vault 相对路径。
- Vault Attachment 不承诺跨设备可用。
- 图片优先提供缩略图和懒加载预览。
- Attachment 删除先解除引用，物理文件清理由保留策略处理。

## Select、MultiSelect 与 Tag

Tag 不是独立 Field Type：

- 一个标签使用 `select`。
- 多个标签使用 `multiSelect`。
- Chip、颜色和标签样式属于 Renderer。
- 跨 Table 共享标签、标签层级和标签权限属于未来 Tag Domain，不属于第一阶段。

## Field Schema 变更

- Field ID 永久稳定。
- 改名不改变 Field ID 和 Cell 值。
- 删除先进入回收站或标记为 deleted。
- 类型变更必须显示迁移预览。
- 无法转换的值进入迁移错误列表。
- Field Config 带有 schema version。
- 未识别的未来 Field Type 不得被静默转换为 Text。

