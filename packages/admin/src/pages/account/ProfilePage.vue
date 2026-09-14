<script setup lang="ts">
import { onMounted, ref } from "vue";
import { NCard, NForm, NFormItem, NInput, NButton, NAvatar, NSpace, useMessage } from "naive-ui";
import { useAdminClient } from "../../client";
import { useAuthSession } from "../../session";
import { errMessage } from "../../labels";

const client = useAdminClient();
const session = useAuthSession();
const { user } = session;
const message = useMessage();
const nickname = ref("");
const bio = ref("");
const file = ref<HTMLInputElement | null>(null);

onMounted(() => {
  nickname.value = user.value?.nickname ?? "";
  bio.value = user.value?.bio ?? "";
});

async function save() {
  try {
    session.setUser(await client.patchMe({ nickname: nickname.value, bio: bio.value }));
    message.success("已保存");
  } catch (e: unknown) {
    message.error(errMessage(e));
  }
}

async function onFile(e: Event) {
  const input = e.target as HTMLInputElement;
  const f = input.files?.[0];
  if (!f) return;
  try {
    session.setUser(await client.uploadAvatar(f));
    message.success("头像已更新");
  } catch (err: unknown) {
    message.error(errMessage(err, "上传失败"));
  }
}
</script>

<template>
  <n-card title="个人资料" class="dauth-page">
    <n-space align="center" style="margin-bottom: 20px">
      <n-avatar :src="user?.avatar_url" :size="64" round>{{ user?.nickname?.slice(0, 1) }}</n-avatar>
      <n-button @click="file?.click()">
        <template #icon><i class="ri-camera-line dauth-ri" /></template>
        更换头像
      </n-button>
      <input ref="file" type="file" accept="image/jpeg,image/png,image/webp" hidden @change="onFile" />
    </n-space>
    <n-form label-placement="top">
      <n-form-item label="昵称"><n-input v-model:value="nickname" /></n-form-item>
      <n-form-item label="简介"><n-input v-model:value="bio" type="textarea" /></n-form-item>
      <n-button type="primary" @click="save">保存</n-button>
    </n-form>
  </n-card>
</template>
