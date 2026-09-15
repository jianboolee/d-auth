<script setup lang="ts">
import { NButton } from "naive-ui";
import { useAuthPaths } from "../../paths";

const paths = useAuthPaths();

/**
 * 「查看」一律新开页面：协议页是独立页面（没有侧栏），配置了外部链接时
 * 它会把访问者直接跳走——在当前页打开就等于把账户中心一起带走。
 */
const docs = [
  { label: "用户协议", href: paths.terms() },
  { label: "隐私政策", href: paths.privacy() },
];
</script>

<template>
  <div class="dauth-page agreements">
    <div class="list">
      <div v-for="doc in docs" :key="doc.href" class="row">
        <span class="row-title">{{ doc.label }}</span>
        <n-button
          tag="a"
          size="small"
          quaternary
          :href="doc.href"
          target="_blank"
          rel="noopener noreferrer"
        >
          查看
        </n-button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.list {
  border-radius: 12px;
  background: var(--dauth-surface);
}
.row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 18px;
}
.row + .row {
  border-top: 1px solid var(--dauth-border);
}
.row-title {
  flex: 1;
  min-width: 0;
  font-size: 14px;
  font-weight: 500;
  color: var(--dauth-text);
}
</style>
