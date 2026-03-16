<script lang="ts">
  let {
    class: className = "h-8 w-8",
    color = "#007a5a",
    title = "OpenStack logo",
  }: {
    class?: string;
    color?: string;
    title?: string;
  } = $props();
</script>

<svg
  viewBox="0 0 256 256"
  role="img"
  aria-label={title}
  class={className}
  style={`color: ${color};`}
  fill="none"
  xmlns="http://www.w3.org/2000/svg"
>
  <path
    fill-rule="evenodd"
    clip-rule="evenodd"
    d="M128 20C95.9675 20 70 45.9675 70 78C70 110.032 95.9675 136 128 136C160.032 136 186 110.032 186 78C186 45.9675 160.032 20 128 20ZM128 54C114.745 54 104 64.7452 104 78C104 91.2548 114.745 102 128 102C141.255 102 152 91.2548 152 78C152 64.7452 141.255 54 128 54Z"
    fill="currentColor"
  />
  <path
    d="M40 126L128 160L216 126"
    stroke="currentColor"
    stroke-width="20"
    stroke-linecap="round"
    stroke-linejoin="round"
  />
  <path
    d="M40 156L128 190L216 156"
    stroke="currentColor"
    stroke-width="20"
    stroke-linecap="round"
    stroke-linejoin="round"
  />
  <path
    d="M40 186L128 220L216 186"
    stroke="currentColor"
    stroke-width="20"
    stroke-linecap="round"
    stroke-linejoin="round"
  />
</svg>
