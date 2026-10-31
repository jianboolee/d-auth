<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";
import {
  NButton,
  NCard,
  NForm,
  NFormItem,
  NInput,
  NInputGroup,
  NSelect,
  useMessage,
} from "naive-ui";
import { useAdminClient } from "../client";
import { errMessage, roleLabel } from "../labels";
import { adminPhoneLooksValid, normalizePhoneInput, randomVirtualPhone } from "../phone";

const props = defineProps<{ basePath?: string }>();
const client = useAdminClient();
const router = useRouter();
const message = useMessage();
const form = reactive({ nickname: "", phone: "", email: "", password: "", roles: ["user"] });
const loading = ref(false);
const generating = ref(false);
// 角色列表从后端取，自定义角色才能被分配
const roleOptions = ref<{ label: string; value: string }[]>([]);

onMounted(async () => {
  try {
    roleOptions.value = (await client.adminRoles()).map((r) => ({
      label: r.name || roleLabel(r.code),
      value: r.code,
    }));
  } catch (e: unknown) {
    message.error(errMessage(e, "角色加载失败"));
  }
});

/** 向服务端要一个未被占用的虚拟号填入表单。失败则退回本地随机值，不阻断建号。 */
async function generateVirtualPhone() {
  generating.value = true;
  try {
    const { phone } = await client.adminGenerateVirtualPhone();
    form.phone = phone;
  } catch (e: unknown) {
    form.phone = randomVirtualPhone();
    message.warning(errMessage(e, "生成虚拟号失败，已填入一个本地随机的 120 号码"));
  } finally {
    generating.value = false;
  }
}

async function submit() {
  const phone = form.phone.trim();
  // 后台建号允许真实号码或虚拟号；空手机号表示改用邮箱建号。
  if (phone && !adminPhoneLooksValid(phone)) {
    message.warning("手机号格式不正确，请填真实号码或 120 开头的虚拟号");
    return;
  }
  loading.value = true;
  try {
    await client.adminCreateUser({ ...form, phone });
    message.success("用户已创建");
    router.push(`${props.basePath ?? "/admin"}/users`);
  } catch (e: unknown) {
    message.error(errMessage(e, "创建失败"));
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <n-card title="创建用户" class="d-auth-admin dauth-page" style="max-width: 560px">
    <n-form label-placement="top">
      <n-form-item label="昵称"><n-input v-model:value="form.nickname" /></n-form-item>
      <n-form-item label="手机">
        <n-input-group>
          <n-input
            :value="form.phone"
            inputmode="tel"
            placeholder="13800138000 或点右侧生成虚拟号"
            @update:value="form.phone = normalizePhoneInput($event)"
          />
          <n-button :loading="generating" @click="generateVirtualPhone">生成虚拟号</n-button>
        </n-input-group>
      </n-form-item>
      <n-form-item label="邮箱"><n-input v-model:value="form.email" /></n-form-item>
      <n-form-item label="密码"><n-input v-model:value="form.password" type="password" placeholder="至少 8 位" /></n-form-item>
      <n-form-item label="角色">
        <n-select v-model:value="form.roles" multiple :options="roleOptions" />
      </n-form-item>
      <n-button type="primary" :loading="loading" @click="submit">
        <template #icon><i class="ri-user-add-line dauth-ri" /></template>
        创建
      </n-button>
    </n-form>
    <p class="hint">
      后台建号允许 120 开头的虚拟号（不止 11 位真实号码）。虚拟号收不到短信验证码，
      只能用来做密码登录；仅用于占位账号。
    </p>
  </n-card>
</template>

<style scoped>
.hint {
  margin: 16px 0 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--dauth-text-secondary, #8c8c8c);
}
</style>
