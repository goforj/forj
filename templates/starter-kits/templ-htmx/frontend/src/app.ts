import "htmx.org"
import "basecoat-css/all"
import "./style.css"

type Controller = (root: HTMLElement) => void
type ThemePreference = "light" | "dark" | "system"
type BootOptions = { force?: boolean }
type BasecoatOptions = { force?: boolean }
type BasecoatRuntime = { initAll: (options?: BasecoatOptions) => void }
type BasecoatSidebar = HTMLElement & { toggle?: () => void }
type BasecoatToastConfig = {
  category: string
  title: string
  description: string
}
type BasecoatToaster = HTMLElement & { toast?: (config: BasecoatToastConfig) => HTMLElement }

declare global {
  interface Window {
    basecoat?: BasecoatRuntime
  }
}

const themeStorageKey = "theme"
const sidebarCollapsedStorageKey = "sidebar-collapsed"
const sidebarCollapsedClass = "sidebar-collapsed"

const controllers: Record<string, Controller> = {
  billingControls: billingControlsController,
  chart: chartController,
  command: commandController,
  contextMenu: contextMenuController,
  menu: menuController,
  otp: otpController,
  resize: resizeController,
  table: tableController,
  theme: themeController,
}

let documentActionsReady = false

function systemPrefersDark() {
  return window.matchMedia("(prefers-color-scheme: dark)").matches
}

function themePreference(): ThemePreference {
  const stored = localStorage.getItem(themeStorageKey)
  if (stored === "light" || stored === "dark" || stored === "system") {
    return stored
  }
  return "system"
}

function resolveTheme(preference: ThemePreference) {
  if (preference === "system") {
    return systemPrefersDark()
  }
  return preference === "dark"
}

function applyTheme(preference: ThemePreference = themePreference()) {
  const dark = resolveTheme(preference)
  document.documentElement.classList.toggle("dark", dark)
  document.documentElement.style.colorScheme = dark ? "dark" : "light"
}

function setThemePreference(preference: ThemePreference) {
  localStorage.setItem(themeStorageKey, preference)
  applyTheme(preference)
}

function storedSidebarCollapsed() {
  try {
    return localStorage.getItem(sidebarCollapsedStorageKey) === "true"
  } catch {
    return false
  }
}

function persistSidebarCollapsed(collapsed: boolean) {
  try {
    localStorage.setItem(sidebarCollapsedStorageKey, String(collapsed))
  } catch {}
}

function setSidebarCollapsed(collapsed: boolean, persist = true) {
  document.documentElement.classList.toggle(sidebarCollapsedClass, collapsed)
  document.querySelector<HTMLElement>(".gf-shell")?.classList.toggle(sidebarCollapsedClass, collapsed)
  if (persist) {
    persistSidebarCollapsed(collapsed)
  }
}

function boot(root: ParentNode = document, options: BootOptions = {}) {
  setupDocumentActions()
  root.querySelectorAll<HTMLElement>("[data-gf-controller]").forEach((element) => {
    const name = element.dataset.gfController
    if (!name || (!options.force && element.dataset.gfControllerReady === "true")) {
      return
    }
    const controller = controllers[name]
    if (!controller) {
      return
    }
    element.dataset.gfControllerReady = "true"
    controller(element)
  })
}

function setupDocumentActions() {
  if (documentActionsReady) {
    return
  }
  documentActionsReady = true

  document.addEventListener("click", (event) => {
    const target = event.target
    if (!(target instanceof Element)) {
      return
    }

    const dialogBackdrop = target.closest<HTMLElement>("[data-gf-dialog-backdrop]")
    if (dialogBackdrop && target === dialogBackdrop) {
      event.preventDefault()
      closeClosestDialog(dialogBackdrop)
      return
    }

    if (target instanceof HTMLDialogElement && target.open) {
      event.preventDefault()
      target.close()
      return
    }

    const dialogTrigger = target.closest<HTMLElement>("[data-gf-open-dialog]")
    if (dialogTrigger) {
      event.preventDefault()
      openDialog(dialogTrigger.dataset.gfOpenDialog)
      return
    }

    const dialogClose = target.closest<HTMLElement>("[data-gf-close-dialog]")
    if (dialogClose) {
      event.preventDefault()
      if (dialogClose.dataset.gfToast) {
        dispatchToast(dialogClose)
      }
      closeClosestDialog(dialogClose)
      return
    }

    const toastTrigger = target.closest<HTMLElement>("[data-gf-toast]")
    if (toastTrigger) {
      event.preventDefault()
      dispatchToast(toastTrigger)
      return
    }

    const sidebarTrigger = target.closest<HTMLElement>("[data-gf-sidebar-toggle]")
    if (sidebarTrigger) {
      event.preventDefault()
      toggleSidebar()
      return
    }

    const exportTrigger = target.closest<HTMLElement>("[data-gf-export-table]")
    if (exportTrigger) {
      event.preventDefault()
      exportTable(exportTrigger.dataset.gfExportTable || "starter-resources")
    }
  })

  document.addEventListener("keydown", (event) => {
    const commandShortcut = (event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k"
    const sidebarShortcut = (event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "b"
    if (!commandShortcut && !sidebarShortcut) {
      return
    }
    event.preventDefault()
    if (commandShortcut) {
      openDialog("gf-command-menu")
      return
    }
    toggleSidebar()
  })
}

function toggleSidebar() {
  if (!window.matchMedia("(min-width: 768px)").matches) {
    const sidebar = document.getElementById("app-sidebar") as BasecoatSidebar | null
    sidebar?.toggle?.()
    return
  }

  const shell = document.querySelector<HTMLElement>(".gf-shell")
  if (!shell) {
    return
  }
  const collapsed = !document.documentElement.classList.contains(sidebarCollapsedClass)
  setSidebarCollapsed(collapsed)
}

function restoreSidebar() {
  setSidebarCollapsed(storedSidebarCollapsed(), false)
}

function openDialog(id?: string) {
  if (!id) {
    return
  }
  const dialog = document.getElementById(id)
  if (!(dialog instanceof HTMLDialogElement)) {
    return
  }
  if (!dialog.open) {
    dialog.showModal()
  }
  const autofocus = dialog.querySelector<HTMLElement>("[data-gf-autofocus], input, button, a")
  autofocus?.focus()
}

function closeClosestDialog(element: Element) {
  const dialog = element.closest("dialog")
  if (dialog instanceof HTMLDialogElement) {
    dialog.close()
  }
}

function dispatchToast(trigger: HTMLElement) {
  const toaster = document.getElementById("toaster") as BasecoatToaster | null
  toaster?.toast?.({
    category: trigger.dataset.gfToast || "info",
    title: trigger.dataset.gfToastTitle || "Action complete",
    description: trigger.dataset.gfToastDescription || "",
  })
}

function billingControlsController(root: HTMLElement) {
  const segments = Array.from(root.querySelectorAll<HTMLButtonElement>("[data-gf-segment]"))
  const seatInput = root.querySelector<HTMLInputElement>("[data-gf-seat-count]")
  const decrement = root.querySelector<HTMLButtonElement>("[data-gf-seat-decrement]")
  const increment = root.querySelector<HTMLButtonElement>("[data-gf-seat-increment]")
  const receiptToggle = root.querySelector<HTMLInputElement>("[data-gf-receipt-toggle]")
  const surface = root.closest("section") || document
  const seatsSummary = surface.querySelector<HTMLElement>("[data-gf-seat-summary]")
  const taxSummary = surface.querySelector<HTMLElement>("[data-gf-seat-tax]")
  const totalSummary = surface.querySelector<HTMLElement>("[data-gf-seat-total]")

  const minSeats = Number(seatInput?.min || "1")
  const maxSeats = Number(seatInput?.max || "25")
  const licensePrice = 299

  const syncSegments = (active: HTMLButtonElement) => {
    segments.forEach((segment) => {
      const selected = segment === active
      segment.dataset.active = String(selected)
      segment.setAttribute("aria-pressed", String(selected))
    })
  }

  const syncSeats = (next: number) => {
    if (!seatInput) {
      return
    }
    const seats = clamp(Math.round(next), minSeats, maxSeats)
    const tax = seats * 3
    seatInput.value = String(seats)
    seatsSummary && (seatsSummary.textContent = String(seats))
    taxSummary && (taxSummary.textContent = `$${tax}`)
    totalSummary && (totalSummary.textContent = `$${licensePrice + tax}`)
    decrement && (decrement.disabled = seats <= minSeats)
    increment && (increment.disabled = seats >= maxSeats)
  }

  segments.forEach((segment) => {
    segment.addEventListener("click", () => {
      syncSegments(segment)
    })
  })

  decrement?.addEventListener("click", () => {
    syncSeats(Number(seatInput?.value || minSeats) - 1)
  })
  increment?.addEventListener("click", () => {
    syncSeats(Number(seatInput?.value || minSeats) + 1)
  })
  seatInput?.addEventListener("input", () => {
    syncSeats(Number(seatInput.value || minSeats))
  })
  seatInput?.addEventListener("blur", () => {
    syncSeats(Number(seatInput.value || minSeats))
  })
  receiptToggle?.addEventListener("change", () => {
    receiptToggle.closest("label")?.setAttribute("data-checked", String(receiptToggle.checked))
  })

  const initialSegment = segments.find((segment) => segment.getAttribute("aria-pressed") === "true") || segments[0]
  if (initialSegment) {
    syncSegments(initialSegment)
  }
  syncSeats(Number(seatInput?.value || minSeats))
  receiptToggle?.closest("label")?.setAttribute("data-checked", String(Boolean(receiptToggle?.checked)))
}

function otpController(root: HTMLElement) {
  const cells = Array.from(root.querySelectorAll<HTMLInputElement>("[data-gf-otp-cell]"))

  const cellGroup = (cell: HTMLInputElement) => cells.filter((candidate) => candidate.name === cell.name)

  const moveTo = (group: HTMLInputElement[], index: number) => {
    const next = group[index]
    next?.focus()
    next?.select()
  }

  cells.forEach((cell) => {
    cell.addEventListener("input", () => {
      const group = cellGroup(cell)
      const index = group.indexOf(cell)
      const digit = cell.value.replace(/\D/g, "").slice(-1)
      cell.value = digit
      if (digit) {
        moveTo(group, index + 1)
      }
    })

    cell.addEventListener("keydown", (event) => {
      const group = cellGroup(cell)
      const index = group.indexOf(cell)
      if (event.key === "Backspace" && !cell.value) {
        event.preventDefault()
        moveTo(group, index - 1)
      }
      if (event.key === "ArrowLeft") {
        event.preventDefault()
        moveTo(group, index - 1)
      }
      if (event.key === "ArrowRight") {
        event.preventDefault()
        moveTo(group, index + 1)
      }
    })

    cell.addEventListener("paste", (event) => {
      event.preventDefault()
      const group = cellGroup(cell)
      const start = group.indexOf(cell)
      const digits = event.clipboardData?.getData("text").replace(/\D/g, "").split("") || []
      digits.forEach((digit, offset) => {
        const target = group[start + offset]
        if (target) {
          target.value = digit
        }
      })
      moveTo(group, Math.min(start + digits.length, group.length - 1))
    })
  })
}

function commandController(root: HTMLElement) {
  const input = root.querySelector<HTMLInputElement>("[data-gf-command-input]")
  const items = Array.from(root.querySelectorAll<HTMLElement>("[data-gf-command-item]"))
  const groups = Array.from(root.querySelectorAll<HTMLElement>("[data-gf-command-group]"))
  const empty = root.querySelector<HTMLElement>("[data-gf-command-empty]")

  const sync = () => {
    if (!input) {
      return
    }
    const query = input.value.trim().toLowerCase()
    let visibleCount = 0
    items.forEach((item) => {
      const visible = query === "" || Boolean(item.textContent?.toLowerCase().includes(query))
      item.hidden = !visible
      if (visible) {
        visibleCount += 1
      }
    })
    groups.forEach((group) => {
      group.hidden = !group.querySelector("[data-gf-command-item]:not([hidden])")
    })
    if (empty) {
      empty.hidden = visibleCount !== 0
    }
  }

  input?.addEventListener("input", sync)

  items.forEach((item) => {
    item.addEventListener("click", () => {
      closeClosestDialog(root)
    })
  })

  sync()
}

function menuController(root: HTMLElement) {
  root.querySelectorAll<HTMLElement>("[data-gf-menu-checkbox]").forEach((item) => {
    item.addEventListener("click", () => {
      item.setAttribute("aria-checked", String(item.getAttribute("aria-checked") !== "true"))
    })
  })

  root.querySelectorAll<HTMLElement>("[data-gf-menu-radio]").forEach((item) => {
    item.addEventListener("click", () => {
      const group = item.dataset.gfMenuRadio
      if (!group) {
        return
      }
      root.querySelectorAll<HTMLElement>(`[data-gf-menu-radio="${group}"]`).forEach((option) => {
        option.setAttribute("aria-checked", String(option === item))
      })
    })
  })
}

function contextMenuController(root: HTMLElement) {
  const surface = root.querySelector<HTMLElement>("[data-gf-context-surface]")
  const menu = root.querySelector<HTMLElement>("[data-gf-context-menu]")
  if (!surface || !menu) {
    return
  }

  const hide = () => {
    menu.hidden = true
  }

  const show = (x: number, y: number) => {
    menu.hidden = false
    const rect = menu.getBoundingClientRect()
    const left = Math.min(x, window.innerWidth - rect.width - 12)
    const top = Math.min(y, window.innerHeight - rect.height - 12)
    menu.style.left = `${Math.max(12, left)}px`
    menu.style.top = `${Math.max(12, top)}px`
    menu.querySelector<HTMLElement>("button, a")?.focus()
  }

  surface.addEventListener("contextmenu", (event) => {
    event.preventDefault()
    show(event.clientX, event.clientY)
  })

  surface.addEventListener("keydown", (event) => {
    if (event.key !== "Enter" && event.key !== " ") {
      return
    }
    event.preventDefault()
    const rect = surface.getBoundingClientRect()
    show(rect.left + 24, rect.top + 24)
  })

  menu.addEventListener("click", (event) => {
    const target = event.target
    if (target instanceof Element && target.closest("button, a")) {
      hide()
    }
  })

  document.addEventListener("click", (event) => {
    const target = event.target
    if (target instanceof Node && !root.contains(target)) {
      hide()
    }
  })

  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      hide()
    }
  })
}

function resizeController(root: HTMLElement) {
  const handle = root.querySelector<HTMLElement>("[data-gf-resize-handle]")
  if (!handle) {
    return
  }

  const min = Number(root.dataset.gfResizeMin || "32")
  const max = Number(root.dataset.gfResizeMax || "68")
  let value = clamp(Number(root.dataset.gfResizeValue || "46"), min, max)

  const setValue = (next: number) => {
    value = clamp(next, min, max)
    root.style.setProperty("--resize-left", `${value}%`)
    handle.setAttribute("aria-valuenow", String(Math.round(value)))
  }

  const valueFromPointer = (clientX: number) => {
    const rect = root.getBoundingClientRect()
    if (rect.width <= 0) {
      return value
    }
    return ((clientX - rect.left) / rect.width) * 100
  }

  handle.addEventListener("pointerdown", (event) => {
    if (window.innerWidth < 768) {
      return
    }
    event.preventDefault()
    handle.setPointerCapture(event.pointerId)
    root.classList.add("is-resizing")
    setValue(valueFromPointer(event.clientX))
  })

  handle.addEventListener("pointermove", (event) => {
    if (!handle.hasPointerCapture(event.pointerId)) {
      return
    }
    setValue(valueFromPointer(event.clientX))
  })

  const stopResize = (event: PointerEvent) => {
    if (handle.hasPointerCapture(event.pointerId)) {
      handle.releasePointerCapture(event.pointerId)
    }
    root.classList.remove("is-resizing")
  }

  handle.addEventListener("pointerup", stopResize)
  handle.addEventListener("pointercancel", stopResize)

  handle.addEventListener("keydown", (event) => {
    let next = value
    if (event.key === "ArrowLeft") {
      next -= event.shiftKey ? 10 : 2
    } else if (event.key === "ArrowRight") {
      next += event.shiftKey ? 10 : 2
    } else if (event.key === "Home") {
      next = min
    } else if (event.key === "End") {
      next = max
    } else {
      return
    }
    event.preventDefault()
    setValue(next)
  })

  setValue(value)
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value))
}

function themeController(root: HTMLElement) {
  const buttons = Array.from(root.querySelectorAll<HTMLButtonElement>("[data-gf-theme-option]"))
  const sync = () => {
    const preference = themePreference()
    buttons.forEach((button) => {
      const active = button.dataset.gfThemeOption === preference
      button.dataset.active = String(active)
      button.setAttribute("aria-pressed", String(active))
    })
  }

  buttons.forEach((button) => {
    button.addEventListener("click", () => {
      const preference = button.dataset.gfThemeOption
      if (preference === "light" || preference === "dark" || preference === "system") {
        setThemePreference(preference)
        sync()
      }
    })
  })
  sync()
}

function chartController(root: HTMLElement) {
  const canvas = root.querySelector<HTMLCanvasElement>("[data-gf-chart]")
  const context = canvas?.getContext("2d")
  if (!canvas || !context) {
    return
  }

  const styles = getComputedStyle(document.documentElement)
  const foreground = styles.getPropertyValue("--foreground").trim() || "#111827"
  const muted = styles.getPropertyValue("--muted-foreground").trim() || "#6b7280"
  const border = styles.getPropertyValue("--border").trim() || "#e5e7eb"
  const primary = styles.getPropertyValue("--primary").trim() || "#111827"
  const chartTwo = styles.getPropertyValue("--chart-2").trim() || primary
  const chartThree = styles.getPropertyValue("--chart-3").trim() || primary
  const values = [42, 64, 58, 81, 76, 94]
  const labels = ["Jan", "Feb", "Mar", "Apr", "May", "Jun"]
  const width = canvas.width
  const height = canvas.height
  const padding = 28
  const max = Math.max(...values)
  const step = (width - padding * 2) / (values.length - 1)
  const points = values.map((value, index) => ({
    x: padding + index * step,
    y: height - padding - (value / max) * (height - padding * 2),
  }))

  context.clearRect(0, 0, width, height)
  context.strokeStyle = border
  context.lineWidth = 1
  for (let i = 0; i < 4; i += 1) {
    const y = padding + i * ((height - padding * 2) / 3)
    context.beginPath()
    context.moveTo(padding, y)
    context.lineTo(width - padding, y)
    context.stroke()
  }

  const gradient = context.createLinearGradient(0, padding, 0, height - padding)
  gradient.addColorStop(0, chartTwo)
  gradient.addColorStop(1, chartThree)
  context.beginPath()
  points.forEach((point, index) => {
    if (index === 0) {
      context.moveTo(point.x, point.y)
      return
    }
    context.lineTo(point.x, point.y)
  })
  context.lineTo(points[points.length - 1].x, height - padding)
  context.lineTo(points[0].x, height - padding)
  context.closePath()
  context.globalAlpha = 0.18
  context.fillStyle = gradient
  context.fill()
  context.globalAlpha = 1

  context.beginPath()
  points.forEach((point, index) => {
    if (index === 0) {
      context.moveTo(point.x, point.y)
      return
    }
    context.lineTo(point.x, point.y)
  })
  context.strokeStyle = primary
  context.lineWidth = 2
  context.stroke()

  context.fillStyle = foreground
  points.forEach((point) => {
    context.beginPath()
    context.arc(point.x, point.y, 3, 0, Math.PI * 2)
    context.fill()
  })

  context.fillStyle = muted
  context.font = "12px system-ui, sans-serif"
  labels.forEach((label, index) => {
    context.fillText(label, padding + index * step - 8, height - 8)
  })
}

function tableController(root: HTMLElement) {
  const input = root.querySelector<HTMLInputElement>("[data-gf-table-filter]")
  const status = root.querySelector<HTMLSelectElement>("[data-gf-table-status]")
  const rows = Array.from(root.querySelectorAll<HTMLTableRowElement>("[data-gf-table-row]"))

  const syncRows = () => {
    const query = input?.value.trim().toLowerCase() || ""
    const statusFilter = status?.value || "all"
    rows.forEach((row) => {
      const matchesQuery = query === "" || Boolean(row.textContent?.toLowerCase().includes(query))
      const rowStatus = row.dataset.gfTableStatus || ""
      const matchesStatus = statusFilter === "all" || rowStatus === statusFilter
      row.hidden = !matchesQuery || !matchesStatus
    })
  }

  input?.addEventListener("input", syncRows)
  status?.addEventListener("change", syncRows)

  root.querySelectorAll<HTMLButtonElement>("[data-gf-table-sort]").forEach((button) => {
    button.addEventListener("click", () => {
      const key = button.dataset.gfTableSort
      const body = rows[0]?.parentElement
      if (!key || !body) {
        return
      }
      const sorted = [...rows].sort((a, b) => {
        const left = a.querySelector<HTMLElement>(`[data-gf-table-value="${key}"]`)?.textContent || ""
        const right = b.querySelector<HTMLElement>(`[data-gf-table-value="${key}"]`)?.textContent || ""
        return left.localeCompare(right)
      })
      sorted.forEach((row) => body.appendChild(row))
    })
  })
}

function exportTable(name: string) {
  const rows = Array.from(document.querySelectorAll<HTMLTableRowElement>("[data-gf-table-row]"))
    .filter((row) => !row.hidden)
  const header = ["Resource", "Status", "Owner", "Route", "Updated"]
  const lines = rows.map((row) => Array.from(row.cells).slice(0, 5).map((cell) => cell.textContent?.trim() || ""))
  const csv = [header, ...lines]
    .map((columns) => columns.map((value) => `"${value.replaceAll('"', '""')}"`).join(","))
    .join("\n")
  const blob = new Blob([csv], { type: "text/csv;charset=utf-8" })
  const url = URL.createObjectURL(blob)
  const link = document.createElement("a")
  link.href = url
  link.download = `${name}.csv`
  link.click()
  URL.revokeObjectURL(url)
}

applyTheme()
window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
  if (themePreference() === "system") {
    applyTheme("system")
  }
})

document.addEventListener("DOMContentLoaded", () => boot())
document.addEventListener("DOMContentLoaded", () => restoreSidebar())
document.body.addEventListener("htmx:afterSwap", (event) => {
  if (event.target instanceof HTMLElement) {
    boot(event.target)
    restoreSidebar()
  }
})
document.body.addEventListener("htmx:historyRestore", () => {
  window.basecoat?.initAll({ force: true })
  boot(document, { force: true })
  restoreSidebar()
})
