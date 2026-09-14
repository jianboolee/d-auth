<script setup lang="ts">
import { onMounted, ref } from "vue";
import { NCard, NForm, NFormItem, NInput, NButton, NAvatar, NSpace, useMessage } from "naive-ui";
import { dauth } from "../../api/client";
import { useAuthStore } from "../../stores/auth";

const auth = useAuthStore();
const message = useMessage();
const nickname = ref("");
const bio = ref("");
const file = ref<HTMLInputElement | null>(null);

onMounted(() => {
  nickname.value = auth.user?.nickname ?? "";
  bio.value = auth.user?.bio ?? "";
});

async function save() {
  auth.user = await dauth.patchMe({ nickname: nickname.value, bio: bio.value });
  message.success("已保存");
}

async function onFile(e: Event) {
  const input = e.target as HTMLInputElement;
  const f = input.files?.[0];
  if (!f) return;
  try {
    auth.user = await dauth.uploadAvatar(f);
    message.success("头像已更新");
  } catch (err: any) {
    message.error(err.message ?? "上传失败");
  }
}
</script>

<template>
  <n-card title="个人资料" style="max-width: 640px">
    <n-space align="center" style="margin-bottom: 20px">
      <n-avatar :src="auth.user?.avatar_url" :size="64" round>{{ auth.user?.nickname?.slice(0, 1) }}</n-avatar>
      <n-button @click="file?.click()">更换头像</n-button>
      <input ref="file" type="file" accept="image/jpeg,image/png,image/webp" hidden @change="onFile" />
    </n-space>
    <n-form label-placement="left" label-width="80">
      <n-form-item label="昵称"><n-input v-model:value="nickname" /></n-form-item>
      <n-form-item label="简介"><n-input v-model:value="bio" type="textarea" /></n-form-item>
      <n-button type="primary" @click="save">保存</n-button>
    </n-form>
  </n-card>
</template>
