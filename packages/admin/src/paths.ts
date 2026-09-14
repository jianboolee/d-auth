import { useRoute } from "vue-router";

export function useEmbed() {
  const route = useRoute();
  return () =>
    route.path.startsWith("/embed") ||
    route.meta.embed === true ||
    route.query.embed === "1" ||
    (typeof window !== "undefined" && window.self !== window.top);
}

export function useAuthPaths() {
  const route = useRoute();
  const embed = () => route.path.startsWith("/embed") || route.meta.embed === true;

  function login() {
    return embed() ? "/embed/login" : "/login";
  }
  function register() {
    return embed() ? "/embed/register" : "/register";
  }
  function forgot() {
    return embed() ? "/embed/forgot" : "/forgot";
  }
  function profile() {
    return embed() ? "/embed/account/profile" : "/account/profile";
  }
  function adminUsers() {
    return embed() ? "/embed/admin/users" : "/admin/users";
  }
  function accountBase() {
    return embed() ? "/embed/account" : "/account";
  }
  function adminBase() {
    return embed() ? "/embed/admin" : "/admin";
  }

  return { embed, login, register, forgot, profile, adminUsers, accountBase, adminBase };
}
