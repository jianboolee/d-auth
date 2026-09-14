<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useAdminClient } from "../../client";
import { isSafeUrl, renderMarkdown, type LegalKind } from "../../legal";
import { errMessage } from "../../labels";

const props = defineProps<{ kind: LegalKind }>();

const client = useAdminClient();
const loading = ref(true);
const failed = ref("");
const html = ref("");

const title = computed(() => (props.kind === "terms" ? "用户协议" : "隐私政策"));

onMounted(async () => {
  try {
    const all = await client.legal();
    const doc = all[props.kind];
    const link = doc?.url?.trim() ?? "";
    if (link) {
      // 已配置外部链接：跳转过去，本页不再渲染正文。
      // 用 replace 而不是 assign，避免用户点返回又回到这里形成死循环。
      if (isSafeUrl(link)) {
        window.location.replace(link);
        return;
      }
      // 服务端已校验过，这里兜底：脏数据不跳转，退回渲染正文。
      failed.value = "配置的链接无效，已忽略";
    }
    const content = doc?.content ?? "";
    if (content) html.value = renderMarkdown(content);
  } catch (e: unknown) {
    failed.value = errMessage(e, "加载失败");
  } finally {
    loading.value = false;
  }
});
</script>

<template>
  <div class="legal">
    <!-- 加载中不渲染任何内容：避免先闪一下占位页再跳转 -->
    <template v-if="!loading">
      <h1>{{ title }}</h1>
      <p v-if="failed" class="notice">{{ failed }}</p>
      <!-- html 已由 renderMarkdown 过 DOMPurify 消毒 -->
      <div v-if="html" class="md" v-html="html" />
      <p v-else-if="!failed" class="notice">内容尚未配置。</p>
    </template>
  </div>
</template>

<style scoped>
.legal {
  max-width: 720px;
  margin: 0 auto;
  padding: 48px 24px;
}
h1 {
  margin: 0 0 20px;
  font-size: 24px;
  font-weight: 600;
}
.notice {
  color: var(--dauth-text-secondary, #8c8c8c);
  font-size: 14px;
}
/* markdown 正文的排版（只在 .md 内生效） */
.md {
  font-size: 14px;
  line-height: 1.75;
  color: var(--dauth-text, #1a1a1a);
  word-break: break-word;
}
.md :deep(h1),
.md :deep(h2),
.md :deep(h3) {
  margin: 24px 0 12px;
  font-weight: 600;
  line-height: 1.4;
}
.md :deep(h1) {
  font-size: 20px;
}
.md :deep(h2) {
  font-size: 17px;
}
.md :deep(h3) {
  font-size: 15px;
}
.md :deep(p),
.md :deep(ul),
.md :deep(ol) {
  margin: 0 0 12px;
}
.md :deep(ul),
.md :deep(ol) {
  padding-left: 22px;
}
.md :deep(li) {
  margin: 4px 0;
}
.md :deep(a) {
  color: var(--dauth-text, #1a1a1a);
  text-decoration: underline;
}
.md :deep(code) {
  padding: 2px 5px;
  border-radius: 4px;
  background: var(--dauth-surface, #f3f3f3);
  font-size: 13px;
}
.md :deep(pre) {
  overflow-x: auto;
  padding: 12px;
  border-radius: 8px;
  background: var(--dauth-surface, #f3f3f3);
}
.md :deep(pre code) {
  padding: 0;
  background: none;
}
.md :deep(blockquote) {
  margin: 0 0 12px;
  padding-left: 12px;
  border-left: 3px solid var(--dauth-border, #ececec);
  color: var(--dauth-text-secondary, #6b6b6b);
}
.md :deep(table) {
  width: 100%;
  border-collapse: collapse;
  margin-bottom: 12px;
}
.md :deep(th),
.md :deep(td) {
  padding: 6px 10px;
  border: 1px solid var(--dauth-border, #ececec);
  text-align: left;
}
.md :deep(hr) {
  border: none;
  border-top: 1px solid var(--dauth-border, #ececec);
  margin: 20px 0;
}
</style>
