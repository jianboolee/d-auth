<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { NButton, NCard, NForm, NFormItem, NInput, NSpace, NTabPane, NTabs, useMessage } from "naive-ui";
import type { LegalDocSet } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../client";
import { invalidateLegalCache, isSafeUrl, renderMarkdown, type LegalKind } from "../legal";
import { errMessage } from "../labels";

const client = useAdminClient();
const message = useMessage();
const loading = ref(false);
const saving = ref(false);
const kind = ref<LegalKind>("terms");

type Draft = { url: string; content: string };
const forms = reactive<Record<LegalKind, Draft>>({
  terms: { url: "", content: "" },
  privacy: { url: "", content: "" },
});

async function load() {
  loading.value = true;
  try {
    const all: LegalDocSet = await client.adminLegal();
    for (const k of ["terms", "privacy"] as LegalKind[]) {
      forms[k].url = all[k]?.url ?? "";
      forms[k].content = all[k]?.content ?? "";
    }
  } catch (e: unknown) {
    message.error(errMessage(e, "加载失败"));
  } finally {
    loading.value = false;
  }
}

const current = computed(() => forms[kind.value]);
const preview = computed(() => (current.value.content ? renderMarkdown(current.value.content) : ""));
// 链接格式即时提示，与后端校验保持一致（只放行 http/https）
const urlInvalid = computed(() => !!current.value.url.trim() && !isSafeUrl(current.value.url));

async function save() {
  if (urlInvalid.value) {
    message.warning("链接必须以 http:// 或 https:// 开头");
    return;
  }
  saving.value = true;
  try {
    await client.adminUpdateLegal(kind.value, {
      url: current.value.url.trim(),
      content: current.value.content,
    });
    // 后台保存后清掉前台缓存，否则下次打开协议页可能仍是旧内容
    invalidateLegalCache();
    message.success("已保存");
  } catch (e: unknown) {
    message.error(errMessage(e, "保存失败"));
  } finally {
    saving.value = false;
  }
}

onMounted(load);
</script>

<template>
  <n-card title="用户协议 / 隐私政策" class="d-auth-admin dauth-page wide">
    <template #header-extra>
      <n-button type="primary" :loading="saving" :disabled="loading" @click="save">保存</n-button>
    </template>

    <n-tabs v-model:value="kind" type="line" animated>
      <n-tab-pane name="terms" tab="用户协议">
        <n-form label-placement="top" :disabled="loading">
          <n-form-item label="外部链接（填写后前台跳转到该地址）">
            <n-input
              v-model:value="forms.terms.url"
              :status="urlInvalid ? 'error' : undefined"
              placeholder="https://example.com/terms，留空则显示下方正文"
            />
          </n-form-item>
          <n-form-item label="正文（Markdown）">
            <n-input
              v-model:value="forms.terms.content"
              type="textarea"
              :rows="14"
              placeholder="# 用户协议&#10;&#10;支持 Markdown：标题、列表、链接、表格等"
            />
          </n-form-item>
        </n-form>
      </n-tab-pane>

      <n-tab-pane name="privacy" tab="隐私政策">
        <n-form label-placement="top" :disabled="loading">
          <n-form-item label="外部链接（填写后前台跳转到该地址）">
            <n-input
              v-model:value="forms.privacy.url"
              :status="urlInvalid ? 'error' : undefined"
              placeholder="https://example.com/privacy，留空则显示下方正文"
            />
          </n-form-item>
          <n-form-item label="正文（Markdown）">
            <n-input
              v-model:value="forms.privacy.content"
              type="textarea"
              :rows="14"
              placeholder="# 隐私政策&#10;&#10;支持 Markdown：标题、列表、链接、表格等"
            />
          </n-form-item>
        </n-form>
      </n-tab-pane>
    </n-tabs>

    <n-space vertical size="small" style="margin-top: 8px">
      <p class="hint">
        链接与正文二选一：填了链接则前台跳转到该地址，否则渲染正文。两者都留空则前台只显示标题占位。
      </p>
      <template v-if="preview">
        <p class="hint">正文预览：</p>
        <!-- preview 已由 renderMarkdown 过 DOMPurify 消毒 -->
        <div class="preview md" v-html="preview" />
      </template>
    </n-space>
  </n-card>
</template>

<style scoped>
.hint {
  margin: 0;
  font-size: 12px;
  color: var(--dauth-text-secondary, #8c8c8c);
}
.preview {
  max-width: 720px;
  padding: 16px;
  border: 1px solid var(--dauth-border, #ececec);
  border-radius: 8px;
  font-size: 14px;
  line-height: 1.75;
  word-break: break-word;
}
.preview :deep(h1),
.preview :deep(h2),
.preview :deep(h3) {
  margin: 20px 0 10px;
  font-weight: 600;
  line-height: 1.4;
}
.preview :deep(h1) {
  font-size: 20px;
}
.preview :deep(h2) {
  font-size: 17px;
}
.preview :deep(h3) {
  font-size: 15px;
}
.preview :deep(p),
.preview :deep(ul),
.preview :deep(ol) {
  margin: 0 0 12px;
}
.preview :deep(ul),
.preview :deep(ol) {
  padding-left: 22px;
}
.preview :deep(li) {
  margin: 4px 0;
}
.preview :deep(code) {
  padding: 2px 5px;
  border-radius: 4px;
  background: var(--dauth-surface, #f3f3f3);
  font-size: 13px;
}
.preview :deep(pre) {
  overflow-x: auto;
  padding: 12px;
  border-radius: 8px;
  background: var(--dauth-surface, #f3f3f3);
}
.preview :deep(pre code) {
  padding: 0;
  background: none;
}
.preview :deep(blockquote) {
  margin: 0 0 12px;
  padding-left: 12px;
  border-left: 3px solid var(--dauth-border, #ececec);
  color: var(--dauth-text-secondary, #6b6b6b);
}
.preview :deep(table) {
  width: 100%;
  border-collapse: collapse;
  margin-bottom: 12px;
}
.preview :deep(th),
.preview :deep(td) {
  padding: 6px 10px;
  border: 1px solid var(--dauth-border, #ececec);
  text-align: left;
}
</style>
