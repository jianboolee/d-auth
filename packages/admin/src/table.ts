import { h } from "vue";
import type { DataTableColumns } from "naive-ui";
import { NButton, NSpace } from "naive-ui";

/**
 * 表格操作列的统一实现。
 *
 * 为什么要抽出来：五个列表页原先各自 `h(NSpace, { size: 4 }, ...)` 拼按钮，
 * 带来两个移动端问题——
 *
 * 1. 间距只有 4px，纯文字按钮的点击区域又窄，手指很容易点到相邻操作上
 *    （编辑/删除挨着）。这里统一给 8px，并配合 CSS 类 .dauth-action 补足
 *    左右内边距，把每个按钮的可触区域撑到接近 44px 的推荐值。
 * 2. API Keys 页曾用「纯图标 + hover tooltip」，触屏没有 hover，用户面对的
 *    是三个没有标签的图标。这里一律用文字，图标只作为可选前缀。
 *
 * 传 null / undefined 表示该操作在当前行不适用（如系统内置角色不可删除），
 * 会被过滤掉，不会留下空白占位。
 */
export type ActionItem = {
  label: string;
  icon?: string;
  /** 对应 NButton 的 type；"default" 表示不带语义色的普通文字按钮。 */
  type?: "primary" | "error" | "warning" | "default";
  /** 二次确认类操作，如删除。仅影响颜色语义时的默认值，不影响行为。 */
  onClick: () => void;
} | null | undefined;

/**
 * 构造操作列单元格。按钮之间用 NSpace 隔开，间距由 size="small" 提供
 * （naive 的 small = 8px），再由 .dauth-action 补左右 padding。
 */
export function renderActions(items: ActionItem[]) {
  const buttons = items
    .filter((it): it is NonNullable<ActionItem> => !!it)
    .map((it) =>
      h(
        NButton,
        {
          text: true,
          // type 未指定时用 default，显式传 undefined 会让 naive 回落到默认灰
          type: it.type === "default" ? undefined : it.type,
          class: "dauth-action",
          onClick: it.onClick,
        },
        {
          default: () => [
            it.icon ? h("i", { class: `${it.icon} dauth-ri` }) : null,
            it.label,
          ],
        },
      ),
    );

  if (buttons.length === 0) return null;
  return h(NSpace, { size: "small", wrap: false }, { default: () => buttons });
}

/** 操作列的通用配置：固定宽度 + 靠右固定，窄屏横向滚动时始终可见。 */
export const ACTION_COLUMN_WIDTH = 140;

/**
 * 按列定义里声明的 width 求和，得到一个贴合实际内容的最小表格宽度。
 *
 * 原先窄屏统一用 640px 的 scroll-x（MOBILE_TABLE_MIN_WIDTH），那是七列全都
 * 显示时的经验值。精简列之后四列实宽只有约 390px，仍按 640 撑会被强制拉宽
 * 一倍，反而多出一段没内容的横向滚动。没有声明 width 的列（会自适应）不参与
 * 计算，此时返回 undefined，交给 naive 自行处理。
 */
export function sumColumnWidth(columns: DataTableColumns<any>, fallback = 480): number {
  const total = columns.reduce((acc, col) => {
    const w = (col as { width?: number | string }).width;
    return typeof w === "number" ? acc + w : acc;
  }, 0);
  return total > 0 ? total : fallback;
}