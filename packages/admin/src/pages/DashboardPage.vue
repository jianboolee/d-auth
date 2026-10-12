<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { NCard, NSkeleton, useMessage } from "naive-ui";
import type { UserStats } from "@donktech/d-auth-sdk";
import { useAdminClient } from "../client";
import { errMessage } from "../labels";

const client = useAdminClient();
const message = useMessage();
const stats = ref<UserStats | null>(null);
const loading = ref(false);

async function load() {
  loading.value = true;
  try {
    stats.value = await client.adminStats();
  } catch (e: unknown) {
    message.error(errMessage(e, "加载失败"));
  } finally {
    loading.value = false;
  }
}

/**
 * 今日相对昨日的环比。
 *
 * 昨日为 0 时不做除法（会是 Infinity）：这时只报绝对增量，或同为 0 就说「持平」。
 * 百分比按绝对值的四舍五入取整，符号单独给，避免出现「+-」。
 */
const delta = computed(() => {
  if (!stats.value) return null;
  const { today, yesterday } = stats.value;
  if (yesterday === 0) {
    if (today === 0) return { text: "持平", dir: "flat" as const };
    return { text: `较昨日 +${today}`, dir: "up" as const };
  }
  const pct = Math.round(((today - yesterday) / yesterday) * 100);
  if (pct === 0) return { text: "持平", dir: "flat" as const };
  return { text: `${pct > 0 ? "+" : ""}${pct}%`, dir: pct > 0 ? ("up" as const) : ("down" as const) };
});

const cards = computed(() => [
  { key: "total", label: "总用户数", icon: "ri-group-line", value: stats.value?.total },
  { key: "today", label: "今日注册", icon: "ri-user-add-line", value: stats.value?.today },
  { key: "yesterday", label: "昨日注册", icon: "ri-calendar-check-line", value: stats.value?.yesterday },
  { key: "active", label: "活跃用户数", icon: "ri-user-follow-line", value: stats.value?.active },
]);

onMounted(load);
</script>

<template>
  <n-card title="仪表盘" class="d-auth-admin dauth-page wide">
    <div class="stat-grid">
      <div v-for="c in cards" :key="c.key" class="stat-card">
        <div class="stat-head">
          <span class="stat-label">{{ c.label }}</span>
          <i :class="`${c.icon} dauth-ri stat-icon`" />
        </div>
        <n-skeleton v-if="loading" text style="width: 60%; height: 32px" />
        <template v-else>
          <div class="stat-value">{{ c.value ?? "—" }}</div>
          <!-- 环比只挂在「今日注册」卡上，昨日为 0 时不显示百分比 -->
          <div v-if="c.key === 'today' && delta" class="stat-delta" :class="`is-${delta.dir}`">
            {{ delta.text }}<span class="stat-delta-hint">较昨日</span>
          </div>
        </template>
      </div>
    </div>
    <p class="hint">
      统计按服务端本地时区的自然日划分，「今日 / 昨日」即服务器所在时区的当天与前一天；已注销的用户不计入。
    </p>
  </n-card>
</template>

<style scoped>
/* 自适应网格：窄屏自动折行，宽屏一行四张 */
.stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 12px;
}
/* 卡片本身无边框（App.vue 把 Card 的 borderColor 设为 transparent），
   这里用极浅底色把四块区域分开，与页面留白区分。 */
.stat-card {
  padding: 16px 18px;
  border-radius: var(--dauth-radius, 10px);
  background: #fafafa;
}
.stat-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.stat-label {
  font-size: 13px;
  color: var(--dauth-text-secondary, #8a8a94);
}
.stat-icon {
  font-size: 18px;
  color: var(--dauth-text-secondary, #8a8a94);
}
.stat-value {
  margin-top: 10px;
  font-size: 28px;
  font-weight: 600;
  line-height: 1.2;
  color: var(--dauth-text, #18181b);
  font-variant-numeric: tabular-nums;
}
.stat-delta {
  margin-top: 6px;
  font-size: 12px;
  display: flex;
  align-items: center;
  gap: 6px;
}
.stat-delta.is-up {
  color: #18a058;
}
.stat-delta.is-down {
  color: #d03050;
}
.stat-delta.is-flat {
  color: var(--dauth-text-secondary, #8a8a94);
}
.stat-delta-hint {
  color: var(--dauth-text-secondary, #8a8a94);
}
.hint {
  margin: 12px 0 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--dauth-text-secondary, #8c8c8c);
}
</style>