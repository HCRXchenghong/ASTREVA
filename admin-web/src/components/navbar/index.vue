<template>
  <div class="navbar">
    <div class="left-side">
      <img class="brand-logo" :src="logoUrl" :alt="brandName" />
      <div class="brand-copy">
        <strong>{{ brandName }}</strong>
        <small>统一控制台</small>
      </div>
      <icon-menu-fold
        v-if="appStore.device === 'mobile'"
        class="drawer-trigger"
        @click="toggleDrawerMenu"
      />
    </div>
    <div class="center-side">
      <Menu v-if="topMenu" />
    </div>
    <ul class="right-side">
      <li>
        <a-tooltip :content="isFullscreen ? '退出全屏' : '全屏显示'">
          <a-button
            class="nav-btn"
            type="outline"
            :shape="'circle'"
            @click="toggleFullScreen"
          >
            <template #icon>
              <icon-fullscreen-exit v-if="isFullscreen" />
              <icon-fullscreen v-else />
            </template>
          </a-button>
        </a-tooltip>
      </li>
      <li>
        <a-dropdown trigger="click">
          <div class="account">
            <a-avatar :size="30" :style="{ backgroundColor: '#165dff' }">
              {{ accountInitial }}
            </a-avatar>
            <span class="account-name">{{ accountName }}</span>
          </div>
          <template #content>
            <a-doption disabled>
              <icon-user />
              <span>{{ accountEmail || '管理员' }}</span>
            </a-doption>
            <a-doption @click="handleLogout">
              <icon-export />
              <span>退出登录</span>
            </a-doption>
          </template>
        </a-dropdown>
      </li>
    </ul>
  </div>
</template>

<script lang="ts" setup>
  import { computed, inject } from 'vue';
  import { useFullscreen } from '@vueuse/core';
  import { useAppStore, useUserStore } from '@/store';
  import useUser from '@/hooks/user';
  import Menu from '@/components/menu/index.vue';

  const appStore = useAppStore();
  const userStore = useUserStore();
  const { logout } = useUser();
  const { isFullscreen, toggle: toggleFullScreen } = useFullscreen();

  const brandName = 'ASTREVA';
  // 品牌资源随控制台一起构建，dev 与生产都走同一个 base 路径。
  const logoUrl = `${import.meta.env.BASE_URL}assets/astreva-logo-transparent.png`;
  const topMenu = computed(() => appStore.topMenu && appStore.menu);
  const accountName = computed(() => userStore.name || '管理员');
  const accountEmail = computed(() => userStore.email || '');
  const accountInitial = computed(() => (accountName.value || 'A').slice(0, 1));

  const handleLogout = () => {
    logout();
  };
  const toggleDrawerMenu = inject('toggleDrawerMenu') as () => void;
</script>

<style scoped lang="less">
  .navbar {
    display: flex;
    justify-content: space-between;
    height: 100%;
    background-color: var(--color-bg-2);
    border-bottom: 1px solid var(--color-border);
  }

  .left-side {
    display: flex;
    align-items: center;
    gap: 10px;
    padding-left: 20px;
  }

  .brand-logo {
    width: 30px;
    height: 30px;
    object-fit: contain;
  }

  .brand-copy {
    display: flex;
    flex-direction: column;
    line-height: 1.15;

    strong {
      color: var(--color-text-1);
      font-size: 15px;
      letter-spacing: 0.06em;
    }

    small {
      color: var(--color-text-3);
      font-size: 11px;
    }
  }

  .drawer-trigger {
    margin-left: 6px;
    color: var(--color-text-1);
    font-size: 22px;
    cursor: pointer;
  }

  .center-side {
    flex: 1;
  }

  .right-side {
    display: flex;
    align-items: center;
    padding-right: 20px;
    list-style: none;

    li {
      display: flex;
      align-items: center;
      padding: 0 8px;
    }

    .nav-btn {
      border-color: rgb(var(--gray-2));
      color: rgb(var(--gray-8));
      font-size: 16px;
    }

    .account {
      display: flex;
      align-items: center;
      gap: 8px;
      cursor: pointer;
    }

    .account-name {
      max-width: 120px;
      overflow: hidden;
      color: var(--color-text-1);
      font-size: 13px;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
  }
</style>
