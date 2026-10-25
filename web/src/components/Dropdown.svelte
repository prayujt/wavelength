<script lang="ts">
  import { Check, ChevronDown } from '@lucide/svelte';
  import { tick } from 'svelte';
  let {
    id,
    label,
    value,
    options,
    disabled = false,
    onchange,
  }: {
    id: string;
    label: string;
    value: string;
    options: { value: string; label: string }[];
    disabled?: boolean;
    onchange: (value: string) => void;
  } = $props();
  let trigger: HTMLButtonElement;
  let menu: HTMLDivElement;
  let open = $state(false);
  let active = $state(0);
  let position = $state({ left: 0, top: 0, width: 0, height: 240 });
  let typed = '';
  let typedAt = 0;
  const selected = $derived(options.find((option) => option.value === value));
  function close() {
    menu?.hidePopover();
    open = false;
  }
  function place() {
    const rect = trigger.getBoundingClientRect();
    const below = window.innerHeight - rect.bottom - 12;
    const above = rect.top - 12;
    const height = Math.min(240, Math.max(below, above));
    position = {
      left: Math.max(
        8,
        Math.min(rect.left, window.innerWidth - rect.width - 8),
      ),
      top:
        below >= Math.min(240, menu.scrollHeight)
          ? rect.bottom + 5
          : Math.max(8, rect.top - Math.min(height, menu.scrollHeight) - 5),
      width: rect.width,
      height,
    };
  }
  async function show() {
    if (disabled || !options.length) return;
    active = Math.max(
      0,
      options.findIndex((option) => option.value === value),
    );
    open = true;
    position = { ...position, width: trigger.getBoundingClientRect().width };
    await tick();
    menu.showPopover();
    place();
    await reveal();
  }
  async function reveal() {
    await tick();
    menu
      .querySelector<HTMLElement>(`[data-index="${active}"]`)
      ?.scrollIntoView({ block: 'nearest' });
  }
  function choose(index = active) {
    if (options[index]) onchange(options[index].value);
    close();
    trigger.focus();
  }
  async function keydown(event: KeyboardEvent) {
    if (event.key === 'Escape' && open) {
      event.preventDefault();
      event.stopPropagation();
      close();
      return;
    }
    if (event.key === 'Tab') {
      if (open) {
        onchange(options[active].value);
        close();
      }
      return;
    }
    if (
      ['ArrowDown', 'ArrowUp', 'Home', 'End', 'Enter', ' '].includes(event.key)
    ) {
      event.preventDefault();
      if (!open) {
        await show();
        if (event.key === 'Home') active = 0;
        else if (event.key === 'End') active = options.length - 1;
      } else if (event.key === 'Enter' || event.key === ' ') {
        choose();
        return;
      } else if (event.key === 'Home') active = 0;
      else if (event.key === 'End') active = options.length - 1;
      else
        active = Math.max(
          0,
          Math.min(
            options.length - 1,
            active + (event.key === 'ArrowDown' ? 1 : -1),
          ),
        );
      await reveal();
    } else if (
      event.key.length === 1 &&
      !event.ctrlKey &&
      !event.metaKey &&
      !event.altKey
    ) {
      event.preventDefault();
      if (!open) await show();
      typed = Date.now() - typedAt > 700 ? event.key : typed + event.key;
      typedAt = Date.now();
      const prefix = typed.toLocaleLowerCase();
      const match = options.findIndex((option) =>
        option.label.toLocaleLowerCase().startsWith(prefix),
      );
      if (match >= 0) {
        active = match;
        await reveal();
      }
    }
  }
</script>

<svelte:window
  onpointerdown={(event) => {
    if (
      open &&
      !menu.contains(event.target as Node) &&
      !trigger.contains(event.target as Node)
    )
      close();
  }}
  onresize={() => {
    if (open) place();
  }}
/>
<button
  bind:this={trigger}
  {id}
  type="button"
  class="dropdown-trigger"
  role="combobox"
  aria-label={label}
  aria-expanded={open}
  aria-controls={`${id}-options`}
  aria-haspopup="listbox"
  aria-activedescendant={open ? `${id}-option-${active}` : undefined}
  {disabled}
  onclick={() => {
    if (open) close();
    else void show();
  }}
  onkeydown={keydown}
  onblur={() => {
    if (open) close();
  }}
  ><span>{selected?.label || 'Choose an option'}</span><ChevronDown
    size={14}
  /></button
>
<div
  bind:this={menu}
  id={`${id}-options`}
  class="dropdown-options"
  popover="manual"
  role="listbox"
  aria-label={label}
  style:left={`${position.left}px`}
  style:top={`${position.top}px`}
  style:width={`${position.width}px`}
  style:max-height={`${position.height}px`}
>
  {#each options as option, index (option.value)}
    <div
      id={`${id}-option-${index}`}
      role="option"
      aria-selected={value === option.value}
      data-index={index}
      class:active={active === index}
      onpointermove={() => (active = index)}
      onpointerdown={(event) => event.preventDefault()}
      onclick={() => choose(index)}
      onkeydown={keydown}
      tabindex="-1"
    >
      <span>{option.label}</span>{#if value === option.value}<Check
          size={14}
        />{/if}
    </div>
  {/each}
</div>
