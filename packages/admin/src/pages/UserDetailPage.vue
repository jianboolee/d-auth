<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  NButton,
  NCard,
  NDescriptions,
  NDescriptionsItem,
  NForm,
  NFormItem,
  NInput,
  NSelect,
  NSpace,
  NTag,
  useMessage,
} from "naive-ui";
import type { Identity, Session, User } from "@donktech/d-auth-sdk";
import { computed } from "vue";
import { useAdminClient } from "../client";
import { formatDateTime } from "../datetime";
import { adminPhoneLooksValid, normalizePhoneInput } from "../phone";
import { useAuthSession } from "../session";
import { useConfirm } from "../confirm";
import { errMessage, identityLabel, roleLabel, statusLabel } from "../labels";

const props = defineProps<{ basePath?: string }>();
const route = useRoute();
const router = useRouter();
const client = useAdminClient();
const message = useMessage();
const confirm = useConfirm();
const session = useAuthSession();
// 后端用 RequireSuperAdmin 守卫，这里只是个显示开关；权限判定以服务端为准。
const isSuper = computed(() => !!session.user.value?.roles?.includes("superadmin"));
/** 与后端 usersvc.DeleteAccountConfirm 必须一致。 */
const DELETE_CONFIRM_TEXT = "确认注销";
const user = ref<User | null>(null);
const identities = ref<Identity[]>([]);
const sessions = ref<Session[]>([]);
const nickname = ref("");
const roles = ref<string[]>([]);
const phone = ref("");
const email = ref("");
const password = ref("");
const roleOptions = ref<{ label: string; value: string }[]>([]);

async function load() {
  const id = String(route.params.id);
  const [detail, rs] = await Promise.all([client.adminUser(id), client.adminRoles()]);
  user.value = detail.user;
  identities.value = detail.identities;
  sessions.value = detail.sessions;
  nickname.value = detail.user.nickname;
  roles.value = detail.user.roles;
  phone.value = detail.user.phone ?? "";
  email.value = detail.user.email ?? "";
  roleOptions.value = rs.map((r) => ({ label: r.name || roleLabel(r.code), value: r.code }));
}

async function save() {
  if (!user.value) return;
  const id = user.value.id;
  // 手机号/邮箱存在 auth_identities 里且有唯一约束，走独立接口；
  // 两者可以一起改，任一冲突则中断并提示是哪一项。
  const nextPhone = phone.value.trim();
  const nextEmail = email.value.trim();
  const phoneChanged = nextPhone !== (user.value.phone ?? "");
  const emailChanged = nextEmail !== (user.value.email ?? "");
  // 后台改绑走宽松校验，与后端 normalize.AdminPhone 对应：虚拟号（120 开头）也能存
  if (phoneChanged && !adminPhoneLooksValid(nextPhone)) {
    message.warning("手机号格式不正确，请填真实号码、+8613800138000 或 120 开头的虚拟号");
    return;
  }
  try {
    if (phoneChanged) {
      user.value = await client.adminSetUserPhone(id, nextPhone);
    }
    if (emailChanged) {
      user.value = await client.adminSetUserEmail(id, nextEmail);
    }
    user.value = await client.adminPatchUser(id, { nickname: nickname.value, roles: roles.value });
    identities.value = (await client.adminUser(id)).identities;
    // 回填后端规范化后的值（如 13800138000 -> +8613800138000），避免输入框与库里不一致
    phone.value = user.value.phone ?? "";
    email.value = user.value.email ?? "";
    message.success("已保存");
  } catch (e: unknown) {
    message.error(errMessage(e, "保存失败"));
  }
}

async function setStatus(status: string) {
  if (!user.value) return;
  user.value = await client.adminPatchUser(user.value.id, { status });
  message.success(status === "active" ? "已启用" : "已停用");
}

async function resetPass() {
  if (!user.value || password.value.length < 8) {
    message.warning("密码至少 8 位");
    return;
  }
  await client.adminResetPassword(user.value.id, password.value);
  password.value = "";
  message.success("密码已重置，其它会话已失效");
}

function deleteUser() {
  if (!user.value) return;
  const u = user.value;
  confirm({
    title: "确认注销用户",
    content: `将注销「${u.nickname}」。该账号将无法登录，其手机号与邮箱会被释放，个人资料被清除。此操作不可恢复。`,
    positiveText: "确认注销",
    onPositiveClick: async () => {
      try {
        await client.adminDeleteUser(u.id, DELETE_CONFIRM_TEXT);
        message.success("账号已注销");
        router.push(`${props.basePath ?? "/admin"}/users`);
      } catch (e: unknown) {
        message.error(errMessage(e, "注销失败"));
      }
    },
  });
}

onMounted(load);
</script>

<template>
  <div class="d-auth-admin dauth-page" v-if="user">
    <n-space vertical size="large">
      <n-button text @click="router.push(`${props.basePath ?? '/admin'}/users`)">
        <template #icon><i class="ri-arrow-left-line dauth-ri" /></template>
        返回列表
      </n-button>
      <n-card title="用户详情">
        <n-descriptions :column="2">
          <n-descriptions-item label="编号">{{ user.id }}</n-descriptions-item>
          <n-descriptions-item label="状态">
            <n-tag :type="user.status === 'active' ? 'success' : 'warning'">{{ statusLabel(user.status) }}</n-tag>
          </n-descriptions-item>
        </n-descriptions>
        <n-form style="margin-top: 20px" label-placement="top">
          <n-form-item label="昵称">
            <n-input v-model:value="nickname" />
          </n-form-item>
          <n-form-item label="手机号">
            <n-input
              :value="phone"
              inputmode="tel"
              placeholder="13800138000 或 +8613800138000"
              @update:value="phone = normalizePhoneInput($event)"
            />
          </n-form-item>
          <n-form-item label="邮箱">
            <n-input v-model:value="email" placeholder="name@example.com" />
          </n-form-item>
          <n-form-item label="角色">
            <n-select v-model:value="roles" multiple :options="roleOptions" />
          </n-form-item>
        </n-form>
        <p class="hint">
          手机号与邮箱全局唯一，管理员可直接改绑（无需验证码）。手机号可用 120 开头的虚拟号，
          这类号码不能登录、不能收短信。
        </p>
        <n-space>
          <n-button type="primary" @click="save">保存</n-button>
          <n-button v-if="user.status === 'active'" @click="setStatus('disabled')">停用</n-button>
          <n-button v-else type="success" @click="setStatus('active')">启用</n-button>
          <!-- 注销不可逆；仅超管可用（服务端同样限超管） -->
          <n-button v-if="isSuper" type="error" ghost @click="deleteUser">注销账号</n-button>
        </n-space>
      </n-card>
      <n-card title="重置密码">
        <n-space>
          <n-input v-model:value="password" type="password" placeholder="新密码" style="width: 240px" />
          <n-button @click="resetPass">重置并踢下线</n-button>
        </n-space>
      </n-card>
      <n-card title="登录标识">
        <div v-for="id in identities" :key="id.id" class="row">{{ identityLabel(id.type) }} · {{ id.identifier }}</div>
        <div v-if="!identities.length" class="dauth-empty">
          <i class="ri-inbox-2-line dauth-ri" />
          暂无数据
        </div>
      </n-card>
      <n-card title="会话">
        <div v-for="s in sessions" :key="s.id" class="row">
          {{ s.device }} · {{ s.ip || "—" }} · {{ formatDateTime(s.created_at) }} ·
          {{ s.revoked_at ? "已失效" : "有效" }}
        </div>
        <div v-if="!sessions.length" class="dauth-empty">
          <i class="ri-inbox-2-line dauth-ri" />
          暂无数据
        </div>
      </n-card>
    </n-space>
  </div>
</template>

<style scoped>
.row {
  padding: 8px 0;
  border-bottom: 1px solid var(--dauth-border, #ececec);
  font-size: 13px;
  color: var(--dauth-text-secondary, #6b6b6b);
}
.hint {
  margin: -4px 0 0;
  font-size: 12px;
  color: var(--dauth-text-secondary, #8c8c8c);
}
</style>
