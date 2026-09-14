import { useDialog } from "naive-ui";

export type ConfirmOptions = {
  title: string;
  content?: string;
  positiveText?: string;
  negativeText?: string;
  onPositiveClick?: () => void | Promise<void>;
};

/**
 * 全局统一的确认弹窗。
 *
 * naive-ui 的 NDialogProvider 不支持设置默认弹窗样式（componentOptions 里
 * 只有 iconPlacement 生效），所以样式统一由这个封装提供：无图标、黑色主按钮、
 * 中等尺寸。各页面请使用 useConfirm() 而不是直接调用 useDialog()，
 * 以保证全站弹窗外观一致。
 */
export function useConfirm() {
  const dialog = useDialog();
  return function confirm(opts: ConfirmOptions) {
    dialog.create({
      title: opts.title,
      content: opts.content,
      positiveText: opts.positiveText ?? "确认",
      negativeText: opts.negativeText ?? "取消",
      // 不显示图标（showIcon 只能逐次传入，无法全局默认）
      showIcon: false,
      // 默认类型是 warning，会渲染橙色按钮；用 default 走中性色
      type: "default",
      positiveButtonProps: { type: "primary", size: "medium" },
      negativeButtonProps: { size: "medium" },
      onPositiveClick: opts.onPositiveClick,
    });
  };
}
