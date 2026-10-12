import { computed, onMounted, onUnmounted, ref } from "vue";
import type { ComputedRef, Ref } from "vue";

/**
 * 移动端断点。与 ShellLayout 收起侧栏、改用抽屉菜单的阈值保持一致：侧栏收起时
 * 表格也该切到移动端布局，两处用同一个数才不会出现「侧栏没了、表格还是宽屏」。
 */
export const MOBILE_BREAKPOINT = 768;

/**
 * 移动端表格的最小宽度。窄屏下把表格撑到这个宽度从而触发横向滚动，宽屏下容器
 * 本身更宽，min-width 不产生影响。
 */
export const MOBILE_TABLE_MIN_WIDTH = 640;

/** 按窗口宽度判断是否处于移动端；无 window（SSR / 构建期）时按桌面处理。 */
export function useIsMobile(breakpoint = MOBILE_BREAKPOINT): Ref<boolean> {
  const isMobile = ref(typeof window !== "undefined" && window.innerWidth < breakpoint);
  const onResize = () => {
    isMobile.value = window.innerWidth < breakpoint;
  };
  onMounted(() => window.addEventListener("resize", onResize));
  onUnmounted(() => window.removeEventListener("resize", onResize));
  return isMobile;
}

/**
 * n-data-table 的 scroll-x 取值：移动端给最小宽度以允许横向滚动，宽屏给
 * undefined 保留原本的自适应布局。
 *
 * naive 的 scroll-x 只给表格设 min-width（不会强制 table-layout: fixed），所以
 * 宽屏下传 undefined 不会凭空多出滚动条，桌面端外观与改动前一致。
 */
export function useTableScrollX(minWidth = MOBILE_TABLE_MIN_WIDTH): ComputedRef<number | undefined> {
  const isMobile = useIsMobile();
  return computed(() => (isMobile.value ? minWidth : undefined));
}