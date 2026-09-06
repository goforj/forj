import {
  ArrowRight,
  BarChart3,
  Bell,
  Blocks,
  BookOpen,
  CalendarDays,
  CheckCircle2,
  ChevronsUpDown,
  ChevronRight,
  CircleDot,
  ClipboardList,
  Command as CommandIcon,
  Database,
  GitFork,
  KeyRound,
  Layers3,
  LayoutDashboard,
  LayoutTemplate,
  LoaderCircle,
  LogOut,
  Lock,
  Mail,
  Moon,
  MoreHorizontal,
  Monitor,
  MousePointerClick,
  PanelLeft,
  Palette,
  Plus,
  Settings,
  ShieldCheck,
  SlidersHorizontal,
  Sparkles,
  Sun,
  User,
  Users,
  Workflow,
} from "lucide-react"
import type { LucideIcon } from "lucide-react"
import * as React from "react"
import { FormEvent, ReactNode, useEffect, useLayoutEffect, useMemo, useState } from "react"
import { Link, Navigate, Route, Routes, useLocation, useNavigate, useSearchParams } from "react-router-dom"
import logo from "./assets/goforj-logo.png"
import {
  Avatar,
  AvatarFallback,
} from "./components/ui/avatar"
import {
  Badge as UiBadge,
} from "./components/ui/badge"
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "./components/ui/breadcrumb"
import { Button as UiButton } from "./components/ui/button"
import { Calendar } from "./components/ui/calendar"
import {
  Card as UiCard,
  CardAction as UiCardAction,
  CardDescription as UiCardDescription,
  CardHeader as UiCardHeader,
  CardTitle as UiCardTitle,
} from "./components/ui/card"
import { Checkbox } from "./components/ui/checkbox"
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from "./components/ui/command"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from "./components/ui/dropdown-menu"
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "./components/ui/accordion"
import {
  Alert as UiAlert,
  AlertDescription as UiAlertDescription,
  AlertTitle as UiAlertTitle,
} from "./components/ui/alert"
import {
  ButtonGroup,
} from "./components/ui/button-group"
import { Input } from "./components/ui/input"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "./components/ui/input-group"
import {
  InputOTP,
  InputOTPGroup,
  InputOTPSlot,
} from "./components/ui/input-otp"
import { Label } from "./components/ui/label"
import { NativeSelect } from "./components/ui/native-select"
import {
  Carousel,
  CarouselContent,
  CarouselItem,
  CarouselNext,
  CarouselPrevious,
} from "./components/ui/carousel"
import {
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationLink,
  PaginationNext,
  PaginationPrevious,
} from "./components/ui/pagination"
import {
  RadioGroup,
  RadioGroupItem,
} from "./components/ui/radio-group"
import { Progress } from "./components/ui/progress"
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "./components/ui/resizable"
import { ScrollArea } from "./components/ui/scroll-area"
import { Separator } from "./components/ui/separator"
import { Skeleton } from "./components/ui/skeleton"
import { Slider } from "./components/ui/slider"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "./components/ui/dialog"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "./components/ui/empty"
import {
  Item,
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemGroup,
  ItemMedia,
  ItemTitle,
} from "./components/ui/item"
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from "./components/ui/popover"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "./components/ui/sheet"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarProvider,
  SidebarRail,
  SidebarTrigger,
  useSidebar,
} from "./components/ui/sidebar"
import { Switch } from "./components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "./components/ui/table"
import {
  Tabs,
  TabsList,
  TabsTrigger,
} from "./components/ui/tabs"
import {
  ToggleGroup,
  ToggleGroupItem,
} from "./components/ui/toggle-group"
import { Textarea } from "./components/ui/textarea"
import {
  AuthUser,
  changePassword,
  currentUser,
  login,
  logout,
  register,
  requestPasswordReset,
  resetPassword,
  updateProfile,
  verifyEmail,
} from "./lib/auth"
import {
  setThemePreference,
  themePreference,
  type ThemePreference,
} from "./lib/theme"

type NavChild = {
  title: string
  path: string
}

type NavItem = {
  title: string
  path: string
  icon: LucideIcon
  children?: NavChild[]
}

const navItems: NavItem[] = [
  { title: "Dashboard", path: "/", icon: LayoutDashboard },
  // goforj:component-library:on:start
  {
    title: "Components",
    path: "/components",
    icon: Blocks,
    children: [
      { title: "Overview", path: "/components/overview" },
      { title: "Forms", path: "/components/forms" },
      { title: "Navigation", path: "/components/navigation" },
      { title: "Overlays", path: "/components/overlays" },
      { title: "Data", path: "/components/data" },
    ],
  },
  // goforj:component-library:end
]

const publicRoutes = new Set(["/login", "/register", "/forgot-password", "/reset-password", "/verify-email"])

function routeTitle(pathname: string) {
  if (pathname === "/") return "Dashboard"
  // goforj:component-library:on:start
  if (pathname.startsWith("/components/overview")) return "Components Overview"
  if (pathname.startsWith("/components/forms")) return "Components Forms"
  if (pathname.startsWith("/components/navigation")) return "Components Navigation"
  if (pathname.startsWith("/components/overlays")) return "Components Overlays"
  if (pathname.startsWith("/components/data")) return "Components Data"
  // goforj:component-library:end
  if (pathname.startsWith("/settings/profile")) return "Profile settings"
  if (pathname.startsWith("/settings/password")) return "Password settings"
  if (pathname.startsWith("/settings/appearance")) return "Appearance settings"
  for (const item of navItems) {
    const child = item.children?.find((candidate) => pathname.startsWith(candidate.path))
    if (child) return child.title
    if (pathname === item.path) return item.title
  }
  return "Dashboard"
}

export function App() {
  const [user, setUser] = useState<AuthUser | null>(null)
  const [loading, setLoading] = useState(true)
  const [commandOpen, setCommandOpen] = useState(false)
  const location = useLocation()
  const navigate = useNavigate()
  const isPublic = publicRoutes.has(location.pathname)
  const title = useMemo(() => routeTitle(location.pathname), [location.pathname])

  useLayoutEffect(() => {
    window.scrollTo(0, 0)
  }, [location.pathname])

  useEffect(() => {
    let cancelled = false
    currentUser().then((loaded) => {
      if (!cancelled) {
        setUser(loaded)
        setLoading(false)
      }
    })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    function handleKeydown(event: KeyboardEvent) {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault()
        setCommandOpen((open) => !open)
      }
      if (event.key === "Escape") {
        setCommandOpen(false)
      }
    }
    window.addEventListener("keydown", handleKeydown)
    return () => window.removeEventListener("keydown", handleKeydown)
  }, [])

  async function handleLogout() {
    await logout()
    setUser(null)
    navigate("/login")
  }

  function selectUserMenu(path: string) {
    navigate(path)
  }

  if (loading) {
    return <div className="loading-screen">Loading GoForj...</div>
  }

  if (isPublic) {
    return (
      <Routes>
        <Route path="/login" element={<LoginView onLogin={setUser} />} />
        <Route path="/register" element={<RegisterView onRegister={setUser} />} />
        <Route path="/forgot-password" element={<ForgotPasswordView />} />
        <Route path="/reset-password" element={<ResetPasswordView />} />
        <Route path="/verify-email" element={<VerifyEmailView onVerified={setUser} />} />
      </Routes>
    )
  }

  if (!user) {
    return <Navigate to="/login" replace />
  }

  return (
    <SidebarProvider className="starter-shell">
      <AppSidebar
        user={user}
        pathname={location.pathname}
        onCommand={() => setCommandOpen(true)}
        onSelectUserMenu={selectUserMenu}
        onLogout={handleLogout}
      />

      <SidebarInset className="main">
        <header className="topbar">
          <SidebarTrigger className="topbar-trigger" />
          <Separator orientation="vertical" className="topbar-separator" />
          <Breadcrumb>
            <BreadcrumbList className="breadcrumb">
              <BreadcrumbItem>Application</BreadcrumbItem>
              <BreadcrumbSeparator />
              <BreadcrumbItem>
                <BreadcrumbPage>{title}</BreadcrumbPage>
              </BreadcrumbItem>
            </BreadcrumbList>
          </Breadcrumb>
        </header>
        <div className="content">
          <Routes>
            <Route path="/" element={<DashboardView />} />
            {/* goforj:component-library:on:start */}
            <Route path="/components" element={<Navigate to="/components/overview" replace />} />
            <Route path="/components/overview" element={<ComponentsOverviewView />} />
            <Route path="/components/forms" element={<ComponentsFormsView />} />
            <Route path="/components/navigation" element={<ComponentsNavigationView />} />
            <Route path="/components/overlays" element={<ComponentsOverlaysView />} />
            <Route path="/components/data" element={<ComponentsDataView />} />
            {/* goforj:component-library:end */}
            <Route path="/settings" element={<Navigate to="/settings/profile" replace />} />
            <Route path="/settings/profile" element={<SettingsProfileView user={user} onUser={setUser} />} />
            <Route path="/settings/password" element={<SettingsPasswordView />} />
            <Route path="/settings/appearance" element={<SettingsAppearanceView />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </div>
      </SidebarInset>
      <CommandMenu
        open={commandOpen}
        onClose={() => setCommandOpen(false)}
        onSelect={(path) => {
          navigate(path)
          setCommandOpen(false)
        }}
      />
    </SidebarProvider>
  )
}

function CommandMenu({ open, onClose, onSelect }: { open: boolean; onClose: () => void; onSelect: (path: string) => void }) {
  const commands = [
    { group: "Pages", title: "Dashboard", description: "Open the generated application overview.", path: "/", shortcut: "G D", icon: LayoutDashboard },
    // goforj:component-library:on:start
    { group: "Components", title: "Components overview", description: "Review the local React component reference.", path: "/components/overview", shortcut: "G C", icon: Blocks },
    { group: "Components", title: "Forms", description: "Inputs, validation, checkout, settings, and token entry examples.", path: "/components/forms", shortcut: "G F", icon: Workflow },
    { group: "Components", title: "Navigation", description: "Menus, command patterns, panes, tabs, and scroll surfaces.", path: "/components/navigation", shortcut: "G N", icon: LayoutTemplate },
    { group: "Components", title: "Overlays", description: "Dialogs, sheets, popovers, drawers, and row actions.", path: "/components/overlays", shortcut: "G O", icon: MousePointerClick },
    { group: "Components", title: "Data", description: "Tables, filters, metrics, pagination, dates, and reporting examples.", path: "/components/data", shortcut: "G T", icon: Database },
    // goforj:component-library:end
    { group: "Settings", title: "Profile settings", description: "Edit the current generated auth profile.", path: "/settings/profile", shortcut: "S P", icon: User },
    { group: "Settings", title: "Password settings", description: "Change the account password.", path: "/settings/password", shortcut: "S W", icon: KeyRound },
    { group: "Settings", title: "Appearance settings", description: "Choose light, dark, or system theme.", path: "/settings/appearance", shortcut: "S A", icon: Palette },
  ]
  const groups = Array.from(new Set(commands.map((command) => command.group)))

  return (
    <CommandDialog open={open} onOpenChange={(nextOpen) => { if (!nextOpen) onClose() }} title="Command Menu" description="Search for a page or action.">
      <CommandInput placeholder="Type a command or search..." />
      <CommandList>
        <CommandEmpty>No results found.</CommandEmpty>
        {groups.map((group) => (
          <CommandGroup key={group} heading={group}>
            {commands.filter((command) => command.group === group).map((command) => {
              const Icon = command.icon
              return (
                <CommandItem
                  key={command.path}
                  value={`${command.title} ${command.description}`}
                  onSelect={() => onSelect(command.path)}
                >
                  <Icon />
                  <span>{command.title}</span>
                  <CommandShortcut>{command.shortcut}</CommandShortcut>
                </CommandItem>
              )
            })}
          </CommandGroup>
        ))}
      </CommandList>
    </CommandDialog>
  )
}

function AppSidebar({
  user,
  pathname,
  onCommand,
  onSelectUserMenu,
  onLogout,
}: {
  user: AuthUser
  pathname: string
  onCommand: () => void
  onSelectUserMenu: (path: string) => void
  onLogout: () => void
}) {
  const { isMobile, setOpenMobile, state, toggleSidebar } = useSidebar()

  function closeMobile() {
    setOpenMobile(false)
  }

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <Link className="team-switcher" to="/" onClick={closeMobile}>
          <span className="team-switcher-mark">
            <img src={logo} alt="GoForj Starter Kit" />
          </span>
          <span className="team-switcher-label">GoForj Starter Kit</span>
        </Link>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>Platform</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {navItems.map((item) => (
                <NavSection key={item.path} item={item} pathname={pathname} onNavigate={closeMobile} collapsed={!isMobile && state === "collapsed"} />
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
        <SidebarGroup>
          <SidebarGroupLabel>Resources</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              <SidebarMenuItem>
                <SidebarMenuButton asChild tooltip="Repository">
                  <a href="https://github.com/goforj/goforj" target="_blank" rel="noreferrer" aria-label="Repository" onClick={closeMobile}>
                    <GitFork />
                    <span>Repository</span>
                  </a>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton asChild tooltip="Documentation">
                  <a href="https://goforj.dev" target="_blank" rel="noreferrer" aria-label="Documentation" onClick={closeMobile}>
                    <BookOpen />
                    <span>Documentation</span>
                  </a>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
        <SidebarGroup className="mt-auto">
          <SidebarGroupLabel>Shortcuts</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              <SidebarMenuItem>
                <SidebarMenuButton tooltip="Command Menu" onClick={onCommand}>
                  <CommandIcon />
                  <span>Command Menu</span>
                  <kbd>CMD + K</kbd>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton tooltip="Collapse Sidebar" onClick={toggleSidebar}>
                  <PanelLeft />
                  <span>Collapse Sidebar</span>
                  <kbd>CMD + B</kbd>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton size="lg" className="account-trigger">
              <Avatar>
                <AvatarFallback>{initials(user)}</AvatarFallback>
              </Avatar>
              <span className="user-copy">
                <span>{displayName(user)}</span>
                <span>{user.email}</span>
              </span>
              <ChevronsUpDown className="ml-auto" />
            </SidebarMenuButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            side={isMobile ? "bottom" : "right"}
            align="end"
            sideOffset={4}
            className="w-56"
          >
            <DropdownMenuLabel className="p-0 font-normal">
              <div className="flex items-center gap-2 px-2 py-1.5 text-left text-sm">
                <Avatar>
                  <AvatarFallback>{initials(user)}</AvatarFallback>
                </Avatar>
                <div className="grid min-w-0 flex-1 gap-0.5 text-left">
                  <span className="truncate text-sm leading-none font-medium">{displayName(user)}</span>
                  <span className="truncate text-xs leading-none text-muted-foreground">{user.email}</span>
                </div>
              </div>
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => onSelectUserMenu("/settings/profile")}><User /> Profile</DropdownMenuItem>
            <DropdownMenuItem onClick={() => onSelectUserMenu("/settings/appearance")}><Palette /> Appearance</DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={onLogout}><LogOut /> Log out</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}

function NavSection({ item, pathname, onNavigate, collapsed }: { item: NavItem; pathname: string; onNavigate?: () => void; collapsed: boolean }) {
  const Icon = item.icon
  const active = item.path === "/" ? pathname === "/" : pathname.startsWith(item.path)
  return (
    <SidebarMenuItem>
      {item.children && collapsed ? (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton asChild isActive={active} tooltip={item.title}>
              <button type="button" title={item.title} aria-label={item.title}>
                <Icon />
                <span>{item.title}</span>
              </button>
            </SidebarMenuButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent side="right" align="start" className="w-52">
            <DropdownMenuLabel>{item.title}</DropdownMenuLabel>
            {item.children.map((child) => (
              <DropdownMenuItem key={child.path} asChild>
                <Link to={child.path} onClick={onNavigate}>{child.title}</Link>
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      ) : (
        <SidebarMenuButton asChild isActive={active} tooltip={item.title}>
          <Link to={item.children?.[0]?.path ?? item.path} title={item.title} aria-label={item.title} onClick={onNavigate}>
            <Icon />
            <span>{item.title}</span>
          </Link>
        </SidebarMenuButton>
      )}
      {item.children && active && !collapsed ? (
        <SidebarMenuSub>
          {item.children.map((child) => (
            <SidebarMenuSubItem key={child.path}>
              <SidebarMenuSubButton asChild isActive={pathname === child.path}>
                <Link to={child.path} onClick={onNavigate}>
                  {child.title}
                </Link>
              </SidebarMenuSubButton>
            </SidebarMenuSubItem>
          ))}
        </SidebarMenuSub>
      ) : null}
    </SidebarMenuItem>
  )
}

function LoginView({ onLogin }: { onLogin: (user: AuthUser) => void }) {
  const [identifier, setIdentifier] = useState("")
  const [password, setPassword] = useState("")
  const [remember, setRemember] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const [error, setError] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const navigate = useNavigate()

  async function submit(event: FormEvent) {
    event.preventDefault()
    setSubmitting(true)
    setError("")
    try {
      const user = await login(identifier, password, remember)
      onLogin(user)
      navigate("/")
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to sign in")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AuthShell eyebrow="Server-authoritative sessions" title="Build the app, not the plumbing.">
      <form onSubmit={submit} className="auth-form">
        <AuthHeader title="Log in to your account" description="Enter your email and password below to log in" />
        <Alert message={error} />
        <Field label="Username or email">
          <input value={identifier} onChange={(event) => setIdentifier(event.target.value)} autoComplete="username" placeholder="email@example.com" />
        </Field>
        <div className="field">
          <span>Password</span>
          <div className="password-field">
            <Input value={password} onChange={(event) => setPassword(event.target.value)} type={showPassword ? "text" : "password"} autoComplete="current-password" placeholder="Password" />
            <UiButton type="button" variant="ghost" size="sm" onClick={() => setShowPassword((value) => !value)}>{showPassword ? "Hide" : "Show"}</UiButton>
          </div>
          <Link className="field-link" to="/forgot-password">Forgot password?</Link>
        </div>
        <Label className="check-row">
          <Checkbox checked={remember} onCheckedChange={(checked) => setRemember(Boolean(checked))} />
          <span>Remember me</span>
        </Label>
        <UiButton className="w-full" disabled={submitting}>
          {submitting ? "Logging in..." : "Log in"}
        </UiButton>
        <div className="auth-signup">
          Don't have an account? <Link to="/register">Sign up</Link>
        </div>
      </form>
    </AuthShell>
  )
}

function RegisterView({ onRegister }: { onRegister: (user: AuthUser) => void }) {
  const [displayName, setDisplayName] = useState("")
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [confirm, setConfirm] = useState("")
  const [verificationLink, setVerificationLink] = useState("")
  const [error, setError] = useState("")
  const [showPassword, setShowPassword] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const navigate = useNavigate()

  async function submit(event: FormEvent) {
    event.preventDefault()
    setError("")
    if (password !== confirm) {
      setError("Passwords do not match.")
      return
    }
    setSubmitting(true)
    try {
      const result = await register(displayName, email, password, true)
      if (result.requires_email_verification) {
        setVerificationLink(`/verify-email?token=${encodeURIComponent(result.verification_token || "")}`)
        return
      }
      if (result.user) {
        onRegister(result.user)
      }
      navigate("/")
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to create your account.")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AuthShell eyebrow="Generated account flow" title="Create an account with the same auth surface as the Vue kit.">
      <form onSubmit={submit} className="auth-form">
        <AuthHeader title="Create your account" description="Enter your details below to sign up" />
        <Alert message={error} />
        {verificationLink ? (
          <div className="success-panel">
            <CheckCircle2 />
            <div>
              <strong>Account created</strong>
              <p>Open the verification route to finish this generated flow.</p>
              <Link to={verificationLink}>Verify email</Link>
            </div>
          </div>
        ) : null}
        <Field label="Name">
          <input value={displayName} onChange={(event) => setDisplayName(event.target.value)} autoComplete="name" placeholder="Full name" />
        </Field>
        <Field label="Email address">
          <input value={email} onChange={(event) => setEmail(event.target.value)} type="email" autoComplete="email" placeholder="email@example.com" />
        </Field>
        <div className="field">
          <span>Password</span>
          <div className="password-field">
            <Input value={password} onChange={(event) => setPassword(event.target.value)} type={showPassword ? "text" : "password"} autoComplete="new-password" placeholder="Password" />
            <UiButton type="button" variant="ghost" size="sm" onClick={() => setShowPassword((value) => !value)}>{showPassword ? "Hide" : "Show"}</UiButton>
          </div>
          <p className="field-help">Password must include at least 8 characters, an uppercase letter, and a symbol.</p>
        </div>
        <Field label="Confirm password">
          <input value={confirm} onChange={(event) => setConfirm(event.target.value)} type={showPassword ? "text" : "password"} autoComplete="new-password" placeholder="Confirm password" />
        </Field>
        <UiButton className="w-full" disabled={submitting}>
          {submitting ? "Creating account..." : "Create account"}
        </UiButton>
        <div className="auth-signup">
          Already have an account? <Link to="/login">Log in</Link>
        </div>
      </form>
    </AuthShell>
  )
}

function ForgotPasswordView() {
  const [identifier, setIdentifier] = useState("")
  const [resetLink, setResetLink] = useState("")
  const [error, setError] = useState("")
  const [submitting, setSubmitting] = useState(false)

  async function submit(event: FormEvent) {
    event.preventDefault()
    setError("")
    setSubmitting(true)
    try {
      const result = await requestPasswordReset(identifier)
      setResetLink(result.reset_token ? `/reset-password?token=${encodeURIComponent(result.reset_token)}` : "")
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to send reset instructions.")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AuthShell eyebrow="Password recovery" title="Reset flows are generated as app-owned source.">
      <form onSubmit={submit} className="auth-form">
        <AuthHeader title="Forgot password" description="Enter your email to receive a password reset link" />
        <Alert message={error} />
        {resetLink ? (
          <div className="success-panel">
            <Mail />
            <div>
              <strong>Reset request ready</strong>
              <p>Use the reset route returned by the local API.</p>
              <Link to={resetLink}>Continue reset</Link>
            </div>
          </div>
        ) : null}
        <Field label="Email address">
          <input value={identifier} onChange={(event) => setIdentifier(event.target.value)} type="email" autoComplete="off" placeholder="email@example.com" />
        </Field>
        <UiButton className="w-full" disabled={submitting}>
          {submitting ? "Sending reset link..." : "Email password reset link"}
        </UiButton>
        <div className="auth-signup">
          Or, return to <Link to="/login">log in</Link>
        </div>
      </form>
    </AuthShell>
  )
}

function ResetPasswordView() {
  const [params] = useSearchParams()
  const [password, setPassword] = useState("")
  const [confirm, setConfirm] = useState("")
  const [success, setSuccess] = useState(false)
  const [error, setError] = useState("")
  const [showPassword, setShowPassword] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const token = params.get("token") || ""

  async function submit(event: FormEvent) {
    event.preventDefault()
    setError("")
    if (password !== confirm) {
      setError("Passwords do not match.")
      return
    }
    setSubmitting(true)
    try {
      await resetPassword(token, password)
      setSuccess(true)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to reset your password.")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AuthShell eyebrow="Credential recovery" title="Password reset includes form, token, error, and success states.">
      <form onSubmit={submit} className="auth-form">
        <AuthHeader title="Reset password" description="Please enter your new password below" />
        <Alert message={error} />
        {success ? (
          <div className="success-panel">
            <CheckCircle2 />
            <div>
              <strong>Password updated</strong>
              <p>You can now sign in with the new password.</p>
              <Link to="/login">Log in</Link>
            </div>
          </div>
        ) : null}
        <div className="field">
          <span>New password</span>
          <div className="password-field">
            <Input value={password} onChange={(event) => setPassword(event.target.value)} type={showPassword ? "text" : "password"} autoComplete="new-password" placeholder="New password" />
            <UiButton type="button" variant="ghost" size="sm" onClick={() => setShowPassword((value) => !value)}>{showPassword ? "Hide" : "Show"}</UiButton>
          </div>
          <p className="field-help">Password must include at least 8 characters, an uppercase letter, and a symbol.</p>
        </div>
        <Field label="Confirm new password">
          <input value={confirm} onChange={(event) => setConfirm(event.target.value)} type={showPassword ? "text" : "password"} autoComplete="new-password" placeholder="Confirm new password" />
        </Field>
        <UiButton className="w-full" disabled={submitting || success}>
          {submitting ? "Resetting password..." : success ? "Password updated" : "Reset password"}
        </UiButton>
        <div className="auth-signup">
          Remembered it? <Link to="/login">Log in</Link>
        </div>
      </form>
    </AuthShell>
  )
}

function VerifyEmailView({ onVerified }: { onVerified: (user: AuthUser) => void }) {
  const [params] = useSearchParams()
  const [message, setMessage] = useState("")
  const [error, setError] = useState("")
  const [success, setSuccess] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const token = params.get("token") || ""

  async function submit() {
    if (!token) {
      setError("Verification link is invalid or incomplete.")
      return
    }
    setSubmitting(true)
    setError("")
    try {
      const user = await verifyEmail(token)
      onVerified(user)
      setSuccess(true)
      setMessage("Your email has been verified. Redirecting you to log in...")
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to verify your email.")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AuthShell eyebrow="Email verification" title="Generated account states stay visible and editable.">
      <section className="auth-form">
        <AuthHeader title="Verify email" description="Confirm your email address to continue" />
        {message ? <p className="status-message success">{message}</p> : null}
        <Alert message={error} />
        <UiButton className="w-full" type="button" disabled={submitting || success} onClick={submit}>
          {submitting ? "Verifying email..." : success ? "Email verified" : "Verify email"}
        </UiButton>
        <div className="auth-signup">
          Back to <Link to="/login">log in</Link>
        </div>
      </section>
    </AuthShell>
  )
}

function DashboardView() {
  // goforj:component-library:on:start
  return (
    <section className="page-stack">
      <HeroCard
        badges={["React application shell", "Local shadcn/ui source"]}
        firstVariant="secondary"
        title="Start from an application shell that already feels production-ready."
        description="React, Vite, Tailwind, and shadcn/ui source files are rendered directly into your app so your team can inspect, adapt, or replace every layer."
      />
      <div className="grid cards-3">
        <FeatureCard icon={Database} title="Backend-integrated" description="Generated routes and controllers give the frontend a server-owned foundation from the start." />
        <FeatureCard icon={ShieldCheck} title="Auth-ready" description="Sign-in, session bootstrap, password reset, and account surfaces can be connected without rebuilding the shell." />
        <FeatureCard icon={Layers3} title="Composable UI foundation" description="The copied shadcn/ui components are local source, so your team can evolve them as product code." />
      </div>
      <section className="card">
        <CardHeader title="Suggested next steps" description="Replace the starter examples with the first workflow your product actually needs." aside={<Badge variant="secondary">App-owned</Badge>} />
        <div className="grid cards-3">
          <StepCard compact step="1" title="Define a domain route" description={<>Create the API surface your first real screen depends on under <code>/api/v1</code>.</>} />
          <StepCard compact step="2" title="Connect authentication" description="Wire sign-in, session bootstrap, and logout to the generated cookie-backed auth endpoints." />
          <StepCard compact step="3" title="Tailor the interface" description="Replace the starter content with product-specific views and extend the local shadcn/ui library as needed." />
        </div>
      </section>
    </section>
  )
  // goforj:component-library:end
  // goforj:component-library:off:start
  return (
    <section className="dashboard-canvas" aria-label="Empty dashboard canvas">
      <div className="dashboard-canvas-grid">
        <div className="dashboard-placeholder" />
        <div className="dashboard-placeholder" />
        <div className="dashboard-placeholder" />
      </div>
      <div className="dashboard-placeholder dashboard-placeholder-large" />
    </section>
  )
  // goforj:component-library:end
}

// goforj:component-library:on:start
function ComponentsOverviewView() {
  const sections = [
    { title: "Forms", path: "/components/forms", icon: Workflow, description: "Validation, field wrappers, selects, tags, OTP, and staged setup flows.", highlights: ["Validated forms", "Combobox and selects", "Tags, OTP, and PIN inputs"] },
    { title: "Navigation", path: "/components/navigation", icon: LayoutTemplate, description: "Menus, disclosures, split panes, scroll containers, and content carousels.", highlights: ["Menubar and dropdowns", "Context and popover menus", "Resizable and scrollable surfaces"] },
    { title: "Overlays", path: "/components/overlays", icon: MousePointerClick, description: "Dialogs, sheets, drawers, command palette, and toast feedback patterns.", highlights: ["Invite and destructive dialogs", "Sheet and drawer patterns", "Command and sonner usage"] },
    { title: "Data", path: "/components/data", icon: Database, description: "Tables, pagination, calendars, and scheduling-oriented reference screens.", highlights: ["Admin-style tables", "Pagination primitives", "Single-date and range calendars"] },
  ]
  return (
    <section className="page-stack">
      <HeroCard
        badges={["Local component reference", "Organized by workflow"]}
        title="Review the local shadcn/ui library as a set of focused reference pages."
        description="The reference is split into focused routes so teams can review one category at a time and lift patterns from realistic examples instead of a single oversized catalog."
      />
      <section className="notice">
        <p>These pages focus on local, product-shaped examples. For the full shadcn/ui documentation and component reference, see <a href="https://ui.shadcn.com/" target="_blank" rel="noreferrer">ui.shadcn.com</a>.</p>
        <UiButton asChild variant="outline">
          <a href="https://ui.shadcn.com/" target="_blank" rel="noreferrer">
            Open docs <ArrowRight />
          </a>
        </UiButton>
      </section>
      <div className="grid cards-4">
        {sections.map((section) => (
          <section className="card tall" key={section.path}>
            <CardHeader icon={section.icon} title={section.title} description={section.description} />
            <ul className="muted-list">
              {section.highlights.map((item) => <li key={item}>{item}</li>)}
            </ul>
            <UiButton asChild variant="secondary" className="mt-auto w-full">
              <Link to={section.path}>
                Open {section.title} <ArrowRight />
              </Link>
            </UiButton>
          </section>
        ))}
      </div>
      <div className="grid split">
        <section className="card">
          <CardHeader title="Surface primitives" description="Reusable primitives still need a small landing area so teams understand the shell and theme language at a glance." />
          <div className="badge-row">
            <Badge>Ready</Badge>
            <Badge variant="secondary">Queued</Badge>
            <Badge variant="outline">Draft</Badge>
            <Badge variant="danger">Blocked</Badge>
          </div>
          <InfoAlert icon={CheckCircle2} title="Everything here ships as local source" description="The generated app owns these examples, so teams can adapt them directly instead of relying on a remote component catalog." />
          <div className="media-tile">Product Hero Media</div>
          <div className="item-list">
            <ItemRow icon={Layers3} title="Application shell" description="Sidebar, app header, auth bootstrap, and local UI source ship together as one coherent starting point." action="Open" />
            <ItemRow icon={Sparkles} title="Theme-aware by default" description="Dark mode follows system preference and the light palette stays readable for day-to-day development." muted />
          </div>
        </section>
        <section className="card">
          <CardHeader title="Loading and empty states" description="Keep a few cross-cutting primitives visible here, then move the deeper walkthroughs into the focused child pages." />
          <div className="badge-row">
            <Badge><LoaderCircle className="spin" /> Syncing</Badge>
            <Badge variant="secondary"><LoaderCircle className="spin" /> Updating</Badge>
            <Badge variant="outline"><LoaderCircle className="spin" /> Loading</Badge>
          </div>
          <SkeletonBlock />
          <EmptyState title="No team members added" description="Invite collaborators when the workspace is ready for shared development." action="Invite collaborators" />
          <EmptyState icon={LoaderCircle} title="Processing request" description="Please wait while we process your request. Do not refresh the page." action="Cancel" variant="outline" spinning />
        </section>
      </div>
    </section>
  )
}

function ComponentsFormsView() {
  return (
    <section className="page-stack">
      <PageIntro badge="Forms" title="Form patterns that match normal application workflows." description="Inputs, validation states, setup steps, preference controls, and recovery flows are grouped into realistic surfaces." />
      <section className="card">
        <CardHeader title="Checkout and billing fields" description="Transactional forms need grouped fields, billing identity, payment details, address capture, and a summary side panel." />
        <div className="grid split">
          <div className="form-grid">
            <div className="form-section">
              <strong>Payment method</strong>
              <p>All transactions are secure and encrypted.</p>
              <div className="form-grid two">
                <Field label="Name on card"><input placeholder="John Doe" /></Field>
                <Field label="Card number"><input placeholder="1234 5678 9012 3456" /></Field>
              </div>
              <div className="form-grid three">
                <Field label="Month"><select defaultValue="06"><option>06</option><option>07</option><option>08</option></select></Field>
                <Field label="Year"><select defaultValue="2028"><option>2028</option><option>2029</option><option>2030</option></select></Field>
                <Field label="CVV"><input placeholder="123" /></Field>
              </div>
            </div>
            <div className="form-section">
              <strong>Billing address</strong>
              <div className="form-grid two">
                <Field label="Street address"><input placeholder="100 Market Street" /></Field>
                <Field label="City"><input placeholder="San Francisco" /></Field>
                <Field label="State / region"><input placeholder="California" /></Field>
                <Field label="Postal code"><input placeholder="94105" /></Field>
              </div>
              <Label className="check-row"><Checkbox defaultChecked /> Same as shipping address</Label>
            </div>
            <div className="form-actions">
              <UiButton>Submit payment</UiButton>
              <UiButton variant="outline">Cancel</UiButton>
            </div>
          </div>
          <aside className="summary-card">
            <strong>Order summary</strong>
            <div><span>Starter kit license</span><span>$299</span></div>
            <div><span>Seats</span><span>5</span></div>
            <div><span>Tax</span><span>$24</span></div>
            <div className="total"><span>Total due today</span><span>$323</span></div>
            <ToggleRow title="Email receipts" description="Send receipts and renewal reminders." checked />
            <ToggleRow title="Annual billing" description="Use the discounted annual plan." />
          </aside>
        </div>
      </section>
      <div className="grid split">
        <section className="card">
          <CardHeader title="Project setup" description="A compact validated form with text, select, checkbox, and textarea patterns." />
          <div className="form-grid">
            <div className="form-grid two">
              <Field label="Project name"><input defaultValue="GoForj Starter Kit" /></Field>
              <Field label="Starter profile"><select defaultValue="react"><option value="react">React dashboard</option><option value="vue">Vue baseline</option><option value="templ">templ + htmx</option></select></Field>
            </div>
            <Field label="Repository URL"><input defaultValue="github.com/acme/mission-control" /></Field>
            <Field label="Notes"><textarea defaultValue="Generate the app-owned shell, auth pages, settings, and local component references." /></Field>
            <div className="grid gap-2">
              <InfoAlert icon={CheckCircle2} title="Slug is available and route-safe." />
              <InfoAlert icon={CircleDot} title="Frontend URL will be read from generated env defaults." />
            </div>
            <div className="form-actions">
              <Label className="check-row"><Checkbox defaultChecked /> Enable generated auth surfaces</Label>
              <UiButton>Save setup</UiButton>
            </div>
          </div>
        </section>
        <PreferenceControlsDemo />
      </div>
      <section className="card">
        <CardHeader title="Staged onboarding" description="Stepper, OTP, and tag-style inputs give teams a starting point for richer flows." />
        <div className="grid cards-3">
          <StepCard step="1" title="Workspace" description="Capture account and project ownership." />
          <StepCard step="2" title="Credentials" description="Collect secrets and service configuration." />
          <StepCard step="3" title="Review" description="Confirm generated routes before launch." />
        </div>
        <OtpPreview value="428916" />
        <div className="tag-row"><Badge>auth</Badge><Badge>web-api</Badge><Badge>scheduler</Badge><Badge>jobs</Badge></div>
      </section>
      <section className="card">
        <CardHeader title="Account and project settings" description="Settings forms need validation copy, profile ownership, access controls, and dangerous actions without turning into a wall of inputs." />
        <div className="settings-reference-grid">
          <div className="setting-row">
            <div><strong>Project status</strong><p>Use a dedicated row for simple on or off preferences.</p></div>
            <ToggleRow title="Enabled" description="Project can receive traffic." checked />
          </div>
          <div className="form-grid two">
            <Field label="Project name"><input defaultValue="GoForj Admin" /></Field>
            <Field label="Owner email"><input defaultValue="team@example.com" /></Field>
            <Field label="API token"><CompoundInput prefix="API" value="token_live_4c4c..." action="Copy" /></Field>
            <Field label="Starter profile"><select defaultValue="react"><option value="react">React application shell</option><option value="vue">Vue application shell</option><option value="templ">templ + htmx</option></select></Field>
          </div>
          <div className="form-grid two">
            <InfoAlert icon={ShieldCheck} title="Validation ready" description="Use this pattern for generated profile forms and account settings." />
            <InfoAlert icon={ShieldCheck} title="Deleting a project should require confirmation and a clear destructive action." destructive />
          </div>
        </div>
      </section>
      <div className="grid cards-3">
        <section className="card">
          <CardHeader title="Input groups" description="Compound controls for product actions and generated commands." />
          <CompoundInput prefix="forj" value="make:controller checkout" />
          <CompoundInput value="checkout@example.dev" action="Invite" />
          <p>Use these for app-prefixed commands, filters, invite flows, and copy-friendly generated values.</p>
        </section>
        <section className="card">
          <CardHeader title="Combobox shape" description="Searchable selection without making users scan long lists." />
          <Command className="rounded-lg border shadow-sm">
            <CommandInput placeholder="Search components..." />
            <CommandList>
              <CommandGroup>
                {["React starter", "Vue starter", "templ + htmx", "API only"].map((item, index) => (
                  <CommandItem key={item}>
                    <Blocks />
                    <span>{item}</span>
                    <CommandShortcut>{index === 0 ? "Selected" : "Pick"}</CommandShortcut>
                  </CommandItem>
                ))}
              </CommandGroup>
            </CommandList>
          </Command>
        </section>
        <section className="card">
          <CardHeader title="Submit states" description="Buttons, destructive actions, and disabled controls in one place." />
          <div className="button-stack">
            <UiButton className="w-full">Create app</UiButton>
            <UiButton className="w-full" variant="outline"><LoaderCircle className="spin" /> Saving changes</UiButton>
            <UiButton className="w-full" variant="destructive">Delete draft</UiButton>
            <UiButton className="w-full" disabled>Waiting for validation</UiButton>
          </div>
        </section>
      </div>
      <div className="grid split">
        <section className="card">
          <CardHeader title="Identity and access verification" description="OTP, backup PINs, invite tags, and account-security prompts belong together when the flow is about trust and activation." />
          <div className="control-list">
            <div className="setting-row compact">
              <div><strong>Two-factor authentication</strong><p>Verify via email or phone number.</p></div>
              <UiButton variant="outline">Enable</UiButton>
            </div>
            <InfoAlert icon={ShieldCheck} title="Your profile has been verified." />
            <Field label="Invite tags">
              <div className="tag-row bordered"><Badge>admin</Badge><Badge>finance</Badge><Badge>ops</Badge><Input placeholder="Add tag..." /></div>
            </Field>
            <Field label="Email verification">
              <OtpPreview value="" slots={4} />
            </Field>
            <Field label="Backup PIN">
              <OtpPreview value="1482" />
            </Field>
          </div>
        </section>
        <section className="card">
          <CardHeader title="Recovery and session controls" description="Keep trusted fallback contacts, recovery routing, and active device actions beside the identity surface instead of burying them inside it." />
          <div className="control-list">
            <div className="form-section">
              <strong>Access recovery</strong>
              <p>Security flows usually need trusted fallback contacts and recovery routing.</p>
              <Field label="Recovery email"><input defaultValue="security@example.com" /></Field>
              <Field label="Recovery phone"><input defaultValue="+1 415 555 0188" /></Field>
              <RadioGroup className="radio-grid" defaultValue="email">
                <Label><RadioGroupItem value="email" /> Email first</Label>
                <Label><RadioGroupItem value="sms" /> SMS first</Label>
              </RadioGroup>
            </div>
            <div className="form-section">
              <strong>Session and device controls</strong>
              <p>Pair switches and device-level actions with account protection surfaces.</p>
              <ToggleRow title="Require device approval" description="New sign-ins must be approved from a trusted session." checked />
              <ToggleRow title="Session timeout" description="Automatically expire inactive sessions after 30 minutes." checked />
              <Item variant="outline">
                <ItemContent>
                  <ItemTitle>MacBook Pro - San Francisco</ItemTitle>
                  <ItemDescription>Last active 2 minutes ago on Chrome 136.</ItemDescription>
                </ItemContent>
                <ItemActions><UiButton variant="outline">Revoke</UiButton></ItemActions>
              </Item>
            </div>
          </div>
        </section>
      </div>
      <EnvironmentControlsDemo />
    </section>
  )
}

function PreferenceControlsDemo() {
  const [themeMode, setThemeMode] = useState("system")
  const [workerLoad, setWorkerLoad] = useState(72)
  const [runtimeMode, setRuntimeMode] = useState("standalone")

  return (
    <section className="card">
      <CardHeader title="Preference controls" description="Use switches, radio choices, and sliders for compact settings." />
      <div className="control-list">
        <ToggleRow title="Email digests" description="Send weekly build summaries." checked />
        <ToggleRow title="Deployment alerts" description="Notify when production deploys finish." checked />
        <ToggleRow title="Experimental UI" description="Preview new starter kit patterns." />
      </div>
      <DemoTabs items={["Light", "Dark", "System"]} value={themeMode} onValueChange={setThemeMode} />
      <div className="range-field">
        <div><strong>Queue workers</strong><span>{workerLoad}%</span></div>
        <Slider className="range" value={[workerLoad]} onValueChange={(value) => setWorkerLoad(value[0] ?? workerLoad)} />
      </div>
      <RadioGroup className="radio-grid" value={runtimeMode} onValueChange={setRuntimeMode}>
        <Label><RadioGroupItem value="standalone" /> Standalone binary</Label>
        <Label><RadioGroupItem value="split" /> Split runtime processes</Label>
      </RadioGroup>
    </section>
  )
}

function EnvironmentControlsDemo() {
  const [environment, setEnvironment] = useState("kubernetes")
  const [gpuCount, setGpuCount] = useState(8)
  const [wallpaperTinting, setWallpaperTinting] = useState(true)
  const [heardFrom, setHeardFrom] = useState(["social-media"])
  const [rolloutMode, setRolloutMode] = useState("public")
  const [featureDensity, setFeatureDensity] = useState(64)
  const [setupView, setSetupView] = useState("preview")

  function setBoundedGpuCount(nextCount: number) {
    setGpuCount(Math.max(0, Math.min(64, nextCount)))
  }

  function updateGpuCount(value: string) {
    const parsedCount = Number.parseInt(value, 10)
    setBoundedGpuCount(Number.isFinite(parsedCount) ? parsedCount : 0)
  }

  return (
    <section className="card">
      <CardHeader title="Environment controls and staged rollout" description="Radio cards, quantity controls, segmented selection, and staged setup examples work best when they are tied to an actual launch workflow." />
      <div className="grid split">
        <div className="control-list">
          <div className="form-section">
            <strong>Compute environment</strong>
            <p>Select the compute environment for your cluster.</p>
            <RadioGroup className="radio-grid" value={environment} onValueChange={setEnvironment}>
              <Label><RadioGroupItem value="kubernetes" /> Kubernetes</Label>
              <Label><RadioGroupItem value="virtual-machine" /> Virtual machine</Label>
            </RadioGroup>
          </div>
          <div className="range-field">
            <div><strong>Number of GPUs</strong><span>{gpuCount}</span></div>
            <div className="quantity-row">
              <UiButton variant="outline" type="button" disabled={gpuCount === 0} onClick={() => setBoundedGpuCount(gpuCount - 1)}>-</UiButton>
              <Input inputMode="numeric" value={gpuCount} onChange={(event) => updateGpuCount(event.target.value)} />
              <UiButton variant="outline" type="button" disabled={gpuCount === 64} onClick={() => setBoundedGpuCount(gpuCount + 1)}>+</UiButton>
            </div>
          </div>
          <Item variant="outline" size="sm">
            <ItemContent>
              <ItemTitle>Wallpaper tinting</ItemTitle>
              <ItemDescription>Allow the wallpaper to be tinted.</ItemDescription>
            </ItemContent>
            <ItemActions><Switch checked={wallpaperTinting} onCheckedChange={setWallpaperTinting} /></ItemActions>
          </Item>
          <Field label="How did you hear about us?">
            <ToggleGroup type="multiple" variant="outline" value={heardFrom} onValueChange={setHeardFrom}>
              <ToggleGroupItem value="social-media">Social media</ToggleGroupItem>
              <ToggleGroupItem value="search-engine">Search engine</ToggleGroupItem>
              <ToggleGroupItem value="referral">Referral</ToggleGroupItem>
              <ToggleGroupItem value="other">Other</ToggleGroupItem>
            </ToggleGroup>
          </Field>
        </div>
        <div className="control-list">
          <div className="form-section">
            <strong>Rollout controls</strong>
            <p>Use sliders and segmented controls to tune staged releases and internal launches.</p>
            <DemoTabs items={["Internal", "Public"]} value={rolloutMode} onValueChange={setRolloutMode} />
            <div className="range-field">
              <div><strong>Feature density</strong><span>{featureDensity}%</span></div>
              <Slider className="range" value={[featureDensity]} onValueChange={(value) => setFeatureDensity(value[0] ?? featureDensity)} />
            </div>
          </div>
          <div className="stepper-list">
            <StepCard step="1" title="Choose the shell" description={`Use ${environment === "kubernetes" ? "cluster" : "machine"} routing for the app shell.`} compact />
            <StepCard step="2" title="Connect auth" description={rolloutMode === "public" ? "Sign-in, me lookup, and logout." : "Restrict access to internal users."} compact />
            <StepCard step="3" title="Ship the first workflow" description={wallpaperTinting ? "Replace examples with tinted product surfaces." : "Replace examples with product behavior."} compact />
          </div>
          <UiCard className="grid gap-3 p-4">
            <div className="flex items-center justify-between gap-4 text-sm"><strong>Project setup</strong><span className="text-muted-foreground">{featureDensity}%</span></div>
            <Progress value={featureDensity} />
            <DemoTabs items={["Preview", "Logs", "Metrics"]} value={setupView} onValueChange={setSetupView} />
          </UiCard>
        </div>
      </div>
    </section>
  )
}

function ComponentsNavigationView() {
  return (
    <section className="page-stack">
      <PageIntro badge="Navigation" title="Navigation and layout patterns that feel like the application." description="Sidebar groups, breadcrumbs, tabs, pagination, and command menu examples mirror the generated shell." />
      <NavigationToolbarDemo />
      <div className="grid split">
        <section className="card">
          <CardHeader title="Sidebar group" description="Use grouped destinations when a workflow owns several child routes." />
          <ItemGroup className="rounded-lg border">
            <Item size="sm" variant="muted"><ItemMedia><LayoutDashboard /></ItemMedia><ItemContent><ItemTitle>Dashboard</ItemTitle></ItemContent></Item>
            <Item size="sm"><ItemMedia><Blocks /></ItemMedia><ItemContent><ItemTitle>Components</ItemTitle></ItemContent></Item>
            <Item size="sm"><ItemContent className="pl-7"><ItemTitle>Forms</ItemTitle></ItemContent></Item>
            <Item size="sm"><ItemContent className="pl-7"><ItemTitle>Navigation</ItemTitle></ItemContent></Item>
            <Item size="sm"><ItemContent className="pl-7"><ItemTitle>Data</ItemTitle></ItemContent></Item>
            <Item size="sm"><ItemMedia><Settings /></ItemMedia><ItemContent><ItemTitle>Settings</ItemTitle></ItemContent></Item>
          </ItemGroup>
        </section>
        <section className="card">
          <CardHeader title="Command movement" description="A command-style list gives agents and users a compact navigation target." />
          <Command className="rounded-lg border shadow-sm">
            <CommandInput placeholder="Search pages..." />
            <CommandList>
              <CommandGroup>
                {["Dashboard", "Components overview", "Profile settings", "Password settings"].map((item) => (
                  <CommandItem key={item}>
                    <CommandIcon />
                    <span>{item}</span>
                    <CommandShortcut>Open</CommandShortcut>
                  </CommandItem>
                ))}
              </CommandGroup>
            </CommandList>
          </Command>
        </section>
      </div>
      <section className="card">
        <CardHeader title="Breadcrumbs, tabs, and pagination" description="Keep repeated navigation controls predictable across operational screens." />
        <div className="breadcrumb demo"><span>Application</span><ChevronRight /><span>Components</span><ChevronRight /><strong>Navigation</strong></div>
        <DemoTabs items={["Overview", "Activity", "Settings"]} />
        <DemoPagination />
      </section>
      <div className="grid cards-3">
        <section className="card">
          <CardHeader title="Dropdown menu" description="A compact action list for app-owned rows and cards." />
          <DropdownMenu>
            <DropdownMenuTrigger asChild><UiButton variant="outline">Open actions</UiButton></DropdownMenuTrigger>
            <DropdownMenuContent>
              <DropdownMenuItem>Open app<DropdownMenuShortcut>Enter</DropdownMenuShortcut></DropdownMenuItem>
              <DropdownMenuItem>Copy command<DropdownMenuShortcut>CMD C</DropdownMenuShortcut></DropdownMenuItem>
              <DropdownMenuItem>View logs<DropdownMenuShortcut>L</DropdownMenuShortcut></DropdownMenuItem>
              <DropdownMenuItem variant="destructive">Archive app<DropdownMenuShortcut>DEL</DropdownMenuShortcut></DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </section>
        <section className="card">
          <CardHeader title="Split panes" description="Use side-by-side navigation for builders and configuration flows." />
          <ResizablePanelGroup className="min-h-48 rounded-lg border" orientation="horizontal">
            <ResizablePanel defaultSize={40} minSize={30}>
              <ItemGroup className="p-3">
                <Item size="sm"><ItemContent><ItemTitle>Routes</ItemTitle></ItemContent></Item>
                {["/checkout", "/orders", "/settings"].map((route) => (
                  <Item key={route} size="sm"><ItemContent><ItemDescription>{route}</ItemDescription></ItemContent></Item>
                ))}
              </ItemGroup>
            </ResizablePanel>
            <ResizableHandle withHandle />
            <ResizablePanel defaultSize={60}>
              <div className="grid gap-2 p-4">
                <strong>Preview</strong>
                <p className="text-sm text-muted-foreground">Selected route metadata, middleware, and controller details.</p>
              </div>
            </ResizablePanel>
          </ResizablePanelGroup>
        </section>
        <section className="card">
          <CardHeader title="Scrollable activity" description="Constrained lists keep dense operational UI from taking over the page." />
          <ScrollArea className="h-48 rounded-lg border">
            <ItemGroup>
              {["Built marketplace", "Migrated database", "Restarted scheduler", "Generated CheckoutController", "Published OpenAPI index"].map((item) => (
                <Item key={item} size="sm"><ItemMedia><CircleDot /></ItemMedia><ItemContent><ItemTitle>{item}</ItemTitle></ItemContent></Item>
              ))}
            </ItemGroup>
          </ScrollArea>
        </section>
      </div>
      <section className="card">
        <CardHeader title="Accordion and disclosure patterns" description="Use smaller disclosures for advanced settings, environment details, and inline help." />
        <Accordion type="single" defaultValue="examples" collapsible>
          <AccordionItem value="examples"><AccordionTrigger>When should examples stay in the app?</AccordionTrigger><AccordionContent><p>Keep examples that act as a useful scaffold for your team, then delete the rest once product-specific replacements exist.</p></AccordionContent></AccordionItem>
          <AccordionItem value="sidebar"><AccordionTrigger>What about the sidebar family?</AccordionTrigger><AccordionContent><p>The generated app shell is already the primary sidebar example, so this page focuses on supporting navigation around it.</p></AccordionContent></AccordionItem>
          <AccordionItem value="commands"><AccordionTrigger>Where should command actions go?</AccordionTrigger><AccordionContent><p>Expose app navigation in the command menu and keep destructive operations in explicit confirmation flows.</p></AccordionContent></AccordionItem>
        </Accordion>
      </section>
      <section className="card">
        <CardHeader title="Card carousel" description="Horizontal navigation works well for quick app or runtime selection." />
        <Carousel className="mx-12">
          <CarouselContent>
            {["Default app", "Marketplace", "Backstage", "Analytics"].map((item, index) => (
              <CarouselItem className="basis-full md:basis-1/2 lg:basis-1/3" key={item}>
                <UiCard className={index === 1 ? "border-primary bg-primary text-primary-foreground p-4" : "p-4"}>
                  <strong>{item}</strong>
                  <span className="text-sm opacity-70">{index === 0 ? "cmd/app" : `cmd/${item.toLowerCase()}`}</span>
                </UiCard>
              </CarouselItem>
            ))}
          </CarouselContent>
          <CarouselPrevious />
          <CarouselNext />
        </Carousel>
      </section>
    </section>
  )
}

function NavigationToolbarDemo() {
  const [position, setPosition] = useState(0)
  const [page, setPage] = useState(1)
  const [accepted, setAccepted] = useState(true)
  const [copilotEnabled, setCopilotEnabled] = useState(false)
  const [status, setStatus] = useState("Dashboard selected.")
  const destinations = ["Dashboard", "Components", "Navigation"]

  function move(nextPosition: number) {
    const boundedPosition = Math.max(0, Math.min(destinations.length - 1, nextPosition))
    setPosition(boundedPosition)
    setStatus(`${destinations[boundedPosition]} selected.`)
  }

  function runAction(action: string) {
    setStatus(`${action} queued for ${destinations[position]}.`)
  }

  return (
    <section className="card">
      <CardHeader title="Toolbar and action clusters" description="Action bars, grouped buttons, pagination controls, and utilities should be evaluated together." />
      <div className="toolbar-row">
        <ButtonGroup>
          <UiButton variant="outline" disabled={position === 0} onClick={() => move(position - 1)}>Back</UiButton>
          <UiButton variant="outline" disabled={position === destinations.length - 1} onClick={() => move(position + 1)}>Forward</UiButton>
        </ButtonGroup>
        <ButtonGroup>
          <UiButton variant="outline" onClick={() => runAction("Archive")}>Archive</UiButton>
          <UiButton variant="outline" onClick={() => runAction("Report")}>Report</UiButton>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <UiButton variant="outline" size="icon" aria-label="More actions"><MoreHorizontal /></UiButton>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={() => runAction("Duplicate")}>Duplicate</DropdownMenuItem>
              <DropdownMenuItem onClick={() => runAction("Copy link")}>Copy link</DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem variant="destructive" onClick={() => runAction("Delete")}>Delete</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </ButtonGroup>
        <ButtonGroup>
          <UiButton variant={copilotEnabled ? "default" : "outline"} onClick={() => {
            setCopilotEnabled((value) => !value)
            setStatus(copilotEnabled ? "Copilot disabled." : "Copilot enabled.")
          }}><CommandIcon /> Copilot</UiButton>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <UiButton variant="outline" size="icon" aria-label="Choose assistant"><ChevronsUpDown /></UiButton>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuLabel>Assistant mode</DropdownMenuLabel>
              <DropdownMenuItem onClick={() => runAction("Review mode")}>Review mode</DropdownMenuItem>
              <DropdownMenuItem onClick={() => runAction("Scaffold mode")}>Scaffold mode</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </ButtonGroup>
      </div>
      <div className="toolbar-row">
        <DemoPagination page={page} onPageChange={(nextPage) => {
          setPage(nextPage)
          setStatus(`Page ${nextPage} selected.`)
        }} />
        <Label className="check-row">
          <Checkbox checked={accepted} onCheckedChange={(checked) => {
            const nextAccepted = checked === true
            setAccepted(nextAccepted)
            setStatus(nextAccepted ? "Terms accepted." : "Terms unchecked.")
          }} />
          I agree to the terms and conditions
        </Label>
      </div>
      <p className="toolbar-status" aria-live="polite">{status}</p>
    </section>
  )
}

function ComponentsOverlaysView() {
  return (
    <section className="page-stack">
      <PageIntro badge="Overlays" title="Dialogs, sheets, popovers, and action menus in product context." description="Overlay examples are shaped around realistic collaboration and administration workflows." />
      <div className="grid cards-3">
        <section className="card">
          <CardHeader icon={Users} title="Invite teammate" description="Dialog surface with role choice and explanatory copy." />
          <Dialog>
            <DialogTrigger asChild><UiButton variant="outline">Open invite dialog</UiButton></DialogTrigger>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Invite teammate</DialogTitle>
                <DialogDescription>Send an invite to join this GoForj workspace.</DialogDescription>
              </DialogHeader>
              <Field label="Email"><input defaultValue="dev@example.com" /></Field>
              <DialogFooter>
                <UiButton>Send invite</UiButton>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </section>
        <section className="card">
          <CardHeader icon={Bell} title="Notification sheet" description="A right-side panel for operational messages." />
          <Sheet>
            <SheetTrigger asChild><UiButton variant="outline">Open notifications</UiButton></SheetTrigger>
            <SheetContent>
              <SheetHeader>
                <SheetTitle>Notifications</SheetTitle>
                <SheetDescription>Recent activity from local development.</SheetDescription>
              </SheetHeader>
              <ItemGroup className="px-4">
                {["Deployment complete", "Password updated", "Queue worker restarted"].map((item) => (
                  <Item key={item} size="sm"><ItemContent><ItemTitle>{item}</ItemTitle></ItemContent><ItemActions><span className="text-sm text-muted-foreground">now</span></ItemActions></Item>
                ))}
              </ItemGroup>
            </SheetContent>
          </Sheet>
        </section>
        <section className="card">
          <CardHeader icon={MoreHorizontal} title="Row actions" description="Dropdown-style actions for tables and lists." />
          <DropdownMenu>
            <DropdownMenuTrigger asChild><UiButton variant="outline">Row actions</UiButton></DropdownMenuTrigger>
            <DropdownMenuContent>
              <DropdownMenuItem>Open</DropdownMenuItem>
              <DropdownMenuItem>Duplicate</DropdownMenuItem>
              <DropdownMenuItem>Archive</DropdownMenuItem>
              <DropdownMenuItem variant="destructive">Delete</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </section>
      </div>
      <section className="card">
        <CardHeader title="Popover details" description="Use popovers for small contextual summaries without leaving the page." />
        <Popover>
          <PopoverTrigger asChild><UiButton variant="outline">Inspect deployment</UiButton></PopoverTrigger>
          <PopoverContent align="start">
            <PopoverHeader>
              <PopoverTitle>Production API</PopoverTitle>
              <PopoverDescription>Healthy, 128 requests in the last hour, p95 latency under 80ms.</PopoverDescription>
            </PopoverHeader>
          </PopoverContent>
        </Popover>
      </section>
    </section>
  )
}

function ComponentsDataView() {
  const [filter, setFilter] = useState("")
  const [sortAsc, setSortAsc] = useState(true)
  const rows = [
    { name: "Dashboard shell", status: "Ready", owner: "Frontend", route: "/", updated: "Just now" },
    { name: "Auth screens", status: "Draft", owner: "Platform", route: "/login", updated: "2h ago" },
    { name: "Metrics cards", status: "Blocked", owner: "API", route: "/metrics", updated: "Yesterday" },
    { name: "Components gallery", status: "Ready", owner: "Frontend", route: "/components", updated: "15m ago" },
    { name: "Settings view", status: "Draft", owner: "Platform", route: "/settings", updated: "1d ago" },
  ]
  const filtered = rows
    .filter((row) => `${row.name} ${row.status} ${row.owner} ${row.route}`.toLowerCase().includes(filter.toLowerCase()))
    .sort((a, b) => sortAsc ? a.name.localeCompare(b.name) : b.name.localeCompare(a.name))

  return (
    <section className="page-stack">
      <PageIntro badge="Data" title="Tables, empty states, filters, and row actions." description="Data pages show local sorting and filtering without hitting the backend, while preserving server-owned data boundaries." />
      <div className="grid cards-4">
        <MetricCard label="Healthy routes" value="24" detail="+3 this week" />
        <MetricCard label="Queued jobs" value="128" detail="12 retrying" />
        <MetricCard label="P95 latency" value="82ms" detail="-18ms from deploy" />
        <MetricCard label="Open reviews" value="6" detail="2 blocked" />
      </div>
      <section className="card">
        <CardHeader title="Component readiness" description="A dense operational table with filter, status badges, and sortable labels." aside={<UiButton variant="outline" onClick={() => setSortAsc((value) => !value)}>Sort {sortAsc ? "Z-A" : "A-Z"}</UiButton>} />
        <div className="table-tools"><Input value={filter} onChange={(event) => setFilter(event.target.value)} placeholder="Filter rows" /></div>
        <Table>
          <TableHeader><TableRow><TableHead>Name</TableHead><TableHead>Status</TableHead><TableHead>Owner</TableHead><TableHead>Route</TableHead><TableHead>Updated</TableHead></TableRow></TableHeader>
          <TableBody>
            {filtered.map((row) => (
              <TableRow key={row.name}>
                <TableCell>{row.name}</TableCell>
                <TableCell><Badge variant={row.status === "Blocked" ? "danger" : row.status === "Draft" ? "secondary" : "default"}>{row.status}</Badge></TableCell>
                <TableCell>{row.owner}</TableCell>
                <TableCell><code>{row.route}</code></TableCell>
                <TableCell>{row.updated}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        <div className="table-footer">
          <span>Showing {filtered.length} of {rows.length} workflows</span>
          <DemoPagination pages={2} />
        </div>
      </section>
      <div className="grid split">
        <section className="card">
          <CardHeader title="Audit log" description="Dense event streams need subdued chrome, clear status emphasis, and predictable row rhythm." aside={<Badge variant="outline">Realtime</Badge>} />
          <Table>
            <TableHeader><TableRow><TableHead>Event</TableHead><TableHead>Actor</TableHead><TableHead>Status</TableHead><TableHead>Time</TableHead></TableRow></TableHeader>
            <TableBody>
              {[
                ["Generated controller", "atlas", "Ready", "now"],
                ["Restarted worker", "admin", "Ready", "4m ago"],
                ["Migration failed", "deploy", "Blocked", "12m ago"],
                ["Published API index", "builder", "Draft", "28m ago"],
              ].map(([event, actor, status, time]) => (
                <TableRow key={event}>
                  <TableCell>{event}</TableCell>
                  <TableCell>{actor}</TableCell>
                  <TableCell><Badge variant={status === "Blocked" ? "danger" : status === "Draft" ? "secondary" : "default"}>{status}</Badge></TableCell>
                  <TableCell>{time}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </section>
        <section className="card">
          <CardHeader title="Invoice summary" description="Financial tables need stronger row separation and right-aligned values." aside={<UiButton variant="outline">Download all</UiButton>} />
          <Table>
            <TableHeader><TableRow><TableHead>Invoice</TableHead><TableHead>Customer</TableHead><TableHead>State</TableHead><TableHead>Amount</TableHead><TableHead /></TableRow></TableHeader>
            <TableBody>
              {[
                ["INV-1001", "Acme Labs", "Paid", "$299"],
                ["INV-1002", "Northstar", "Open", "$499"],
                ["INV-1003", "Launchpad", "Void", "$0"],
              ].map(([number, customer, state, amount]) => (
                <TableRow key={number}>
                  <TableCell>{number}</TableCell>
                  <TableCell>{customer}</TableCell>
                  <TableCell><Badge variant={state === "Void" ? "secondary" : "outline"}>{state}</Badge></TableCell>
                  <TableCell>{amount}</TableCell>
                  <TableCell><UiButton variant="ghost">View</UiButton></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </section>
      </div>
      <div className="grid split">
        <section className="card">
          <CardHeader title="Calendar schedule" description="Calendar blocks help teams review generated schedules and releases." />
          <CalendarBlock />
        </section>
        <section className="card">
          <CardHeader title="Runtime distribution" description="Compact lists make app-scoped operational data easy to scan." />
          <div className="item-list">
            <ItemRow icon={BarChart3} title="HTTP runtime" description="24 routes registered, 18.2k requests in the last hour." action="Inspect" />
            <ItemRow icon={Workflow} title="Jobs runtime" description="128 queued jobs, 12 retries, 3 failed after max attempts." action="Open" />
            <ItemRow icon={CalendarDays} title="Scheduler runtime" description="9 schedules active, next run in 12 minutes." muted />
          </div>
        </section>
      </div>
      <div className="grid split">
        <EmptyState icon={ClipboardList} title="No pending reviews" description="Filtered result sets should provide a clear next action." action="Create review" />
        <section className="card"><CardHeader title="Skeleton loading" description="Use compact loading states before data arrives." /><SkeletonBlock /></section>
      </div>
      <section className="card">
        <CardHeader title="Reporting snapshots" description="Small summary cards help table and calendar sections feel like a dashboard instead of isolated widgets." />
        <div className="grid cards-3">
          <MetricCard label="Daily events" value="18.4k" detail="Across API, auth, and renderer flows." />
          <MetricCard label="Failed jobs" value="12" detail="Down from 31 after the last deploy." />
          <MetricCard label="Median latency" value="84ms" detail="Healthy for the current load profile." />
        </div>
      </section>
    </section>
  )
}

// goforj:component-library:end
function SettingsProfileView({ user, onUser }: { user: AuthUser; onUser: (user: AuthUser) => void }) {
  const [name, setName] = useState(displayName(user))
  const [email, setEmail] = useState(user.email)
  const [message, setMessage] = useState("")
  const [saving, setSaving] = useState(false)

  async function submit(event: FormEvent) {
    event.preventDefault()
    setMessage("")
    setSaving(true)
    try {
      const updated = await updateProfile(name, email)
      onUser(updated)
      setMessage("Profile updated.")
    } catch (err) {
      setMessage(err instanceof Error ? err.message : "Unable to update profile.")
    } finally {
      setSaving(false)
    }
  }

  return (
    <SettingsLayout>
      <form className="settings-form" onSubmit={submit}>
        <SettingsHeader title="Profile" description="Update the account details exposed by generated auth." />
        <Field label="Name"><input value={name} onChange={(event) => setName(event.target.value)} autoComplete="name" /></Field>
        <Field label="Email address"><input value={email} onChange={(event) => setEmail(event.target.value)} type="email" autoComplete="username" /></Field>
        <p className="muted-copy">{user.email_verified_at ? "Your email address is verified and active for account recovery, sign-in alerts, and product notifications." : "Your email address is active for sign-in and notifications. Request verification after changing it."}</p>
        <StatusMessage message={message} />
        <UiButton disabled={saving}>{saving ? "Saving..." : "Save"}</UiButton>
      </form>
    </SettingsLayout>
  )
}

function SettingsPasswordView() {
  const [current, setCurrent] = useState("")
  const [password, setPassword] = useState("")
  const [confirm, setConfirm] = useState("")
  const [message, setMessage] = useState("")
  const [saving, setSaving] = useState(false)

  async function submit(event: FormEvent) {
    event.preventDefault()
    setMessage("")
    if (password !== confirm) {
      setMessage("Passwords do not match.")
      return
    }
    setSaving(true)
    try {
      await changePassword(current, password)
      setMessage("Password updated.")
      setCurrent("")
      setPassword("")
      setConfirm("")
    } catch (err) {
      setMessage(err instanceof Error ? err.message : "Unable to update password.")
    } finally {
      setSaving(false)
    }
  }

  return (
    <SettingsLayout>
      <form className="settings-form" onSubmit={submit}>
        <SettingsHeader title="Password" description="Change the password used by generated cookie-backed auth." />
        <Field label="Current password"><input value={current} onChange={(event) => setCurrent(event.target.value)} type="password" autoComplete="current-password" /></Field>
        <Field label="New password"><input value={password} onChange={(event) => setPassword(event.target.value)} type="password" autoComplete="new-password" /></Field>
        <Field label="Confirm password"><input value={confirm} onChange={(event) => setConfirm(event.target.value)} type="password" autoComplete="new-password" /></Field>
        <StatusMessage message={message} />
        <UiButton disabled={saving}>{saving ? "Saving..." : "Save password"}</UiButton>
      </form>
    </SettingsLayout>
  )
}

function SettingsAppearanceView() {
  const [theme, setTheme] = useState<ThemePreference>(() => themePreference())
  const options: { label: string; value: ThemePreference; icon: LucideIcon }[] = [
    { label: "Light", value: "light", icon: Sun },
    { label: "Dark", value: "dark", icon: Moon },
    { label: "System", value: "system", icon: Monitor },
  ]

  function selectTheme(value: ThemePreference) {
    setTheme(value)
    setThemePreference(value)
  }

  return (
    <SettingsLayout>
      <div className="settings-form">
        <SettingsHeader title="Appearance settings" description="Update your account's appearance settings." />
        <div className="theme-toggle">
          {options.map((option) => {
            const Icon = option.icon
            return (
            <UiButton key={option.value} variant={theme === option.value ? "default" : "outline"} onClick={() => selectTheme(option.value)}>
              <Icon />
              {option.label}
            </UiButton>
            )
          })}
        </div>
      </div>
    </SettingsLayout>
  )
}

function SettingsLayout({ children }: { children: ReactNode }) {
  const location = useLocation()
  const settingsNavItems = [
    { title: "Profile", path: "/settings/profile" },
    { title: "Password", path: "/settings/password" },
    { title: "Appearance", path: "/settings/appearance" },
  ]

  return (
    <section className="settings-page">
      <div className="settings-page-header">
        <h1>Settings</h1>
        <p>Manage your profile and account settings</p>
      </div>
      <div className="settings-shell">
        <aside className="settings-nav" aria-label="Settings">
          {settingsNavItems.map((item) => (
            <Link key={item.path} className={location.pathname === item.path ? "active" : ""} to={item.path}>{item.title}</Link>
          ))}
        </aside>
        <section className="settings-content">{children}</section>
      </div>
    </section>
  )
}

function AuthShell({ children }: { eyebrow: string; title: string; children: ReactNode }) {
  return (
    <div className="auth-shell">
      <section className="auth-main">
        <div className="auth-panel">
          <Link to="/" className="auth-logo">
            <img src={logo} alt="GoForj Starter Kit" />
            <span className="sr-only">GoForj Starter Kit</span>
          </Link>
          {children}
        </div>
      </section>
    </div>
  )
}

function AuthHeader({ title, description }: { title: string; description: string }) {
  return <div className="auth-header"><h1>{title}</h1><p>{description}</p></div>
}

function HeroCard({ badges, title, description, firstVariant = "default" }: { badges: string[]; title: string; description: string; firstVariant?: "default" | "secondary" }) {
  return (
    <section className="hero-card">
      <div className="badge-row">{badges.map((badge, index) => <Badge key={badge} variant={index === 0 ? firstVariant : "outline"}>{badge}</Badge>)}</div>
      <h1>{title}</h1>
      <p>{description}</p>
    </section>
  )
}

function PageIntro({ badge, title, description }: { badge: string; title: string; description: string }) {
  return <HeroCard badges={[badge, "Product-shaped examples"]} title={title} description={description} />
}

function FeatureCard({ icon: Icon, title, description }: { icon: LucideIcon; title: string; description: string }) {
  return <UiCard className="card feature-card"><div className="icon-tile"><Icon /></div><h2>{title}</h2><p>{description}</p></UiCard>
}

function StepCard({ step, title, description, compact = false }: { step: string; title: string; description: ReactNode; compact?: boolean }) {
  return (
    <Item variant="outline" size={compact ? "sm" : "default"}>
      <ItemMedia>
        <UiBadge variant="secondary">{step}</UiBadge>
      </ItemMedia>
      <ItemContent>
        <ItemTitle>{title}</ItemTitle>
        <ItemDescription>{description}</ItemDescription>
      </ItemContent>
    </Item>
  )
}

function CardHeader({ icon: Icon, title, description, aside }: { icon?: LucideIcon; title: string; description?: string; aside?: ReactNode }) {
  return (
    <UiCardHeader className="card-header">
      <div>
        {Icon ? <div className="title-row"><Icon /><UiCardTitle>{title}</UiCardTitle></div> : <UiCardTitle>{title}</UiCardTitle>}
        {description ? <UiCardDescription>{description}</UiCardDescription> : null}
      </div>
      {aside ? <UiCardAction>{aside}</UiCardAction> : null}
    </UiCardHeader>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return <Label className="field"><span>{label}</span>{renderFieldControl(children)}</Label>
}

function CompoundInput({ prefix, value, action, placeholder }: { prefix?: string; value?: string; action?: string; placeholder?: string }) {
  return (
    <InputGroup>
      {prefix ? <InputGroupAddon><span>{prefix}</span></InputGroupAddon> : null}
      <InputGroupInput defaultValue={value} placeholder={placeholder} />
      {action ? <InputGroupAddon align="inline-end"><InputGroupButton variant="outline">{action}</InputGroupButton></InputGroupAddon> : null}
    </InputGroup>
  )
}

function OtpPreview({ value, slots = value.length }: { value: string; slots?: number }) {
  const [otpValue, setOtpValue] = useState(value)

  return (
    <InputOTP maxLength={slots} value={otpValue} onChange={setOtpValue}>
      <InputOTPGroup>
        {Array.from({ length: slots }, (_, index) => <InputOTPSlot key={index} index={index} />)}
      </InputOTPGroup>
    </InputOTP>
  )
}

function InfoAlert({ icon: Icon, title, description, destructive = false }: { icon: LucideIcon; title: string; description?: string; destructive?: boolean }) {
  return (
    <UiAlert variant={destructive ? "destructive" : "default"}>
      <Icon />
      <UiAlertTitle>{title}</UiAlertTitle>
      {description ? <UiAlertDescription>{description}</UiAlertDescription> : null}
    </UiAlert>
  )
}

function Badge({ children, variant = "default" }: { children: ReactNode; variant?: "default" | "secondary" | "outline" | "danger" }) {
  return <UiBadge variant={variant === "danger" ? "destructive" : variant}>{children}</UiBadge>
}

function Alert({ message }: { message: string }) {
  return message ? <UiAlert variant="destructive"><UiAlertDescription>{message}</UiAlertDescription></UiAlert> : null
}

function StatusMessage({ message }: { message: string }) {
  return message ? <p className="status-message">{message}</p> : null
}

function ItemRow({ icon: Icon, title, description, action, muted = false }: { icon: LucideIcon; title: string; description: string; action?: string; muted?: boolean }) {
  return (
    <Item variant={muted ? "muted" : "outline"}>
      <ItemMedia variant="icon"><Icon /></ItemMedia>
      <ItemContent>
        <ItemTitle>{title}</ItemTitle>
        <ItemDescription>{description}</ItemDescription>
      </ItemContent>
      {action ? <ItemActions><UiButton variant="ghost">{action}</UiButton></ItemActions> : null}
    </Item>
  )
}

function EmptyState({ icon: Icon, title, description, action, variant = "primary", spinning = false }: { icon?: LucideIcon; title: string; description: string; action: string; variant?: "primary" | "outline"; spinning?: boolean }) {
  return (
    <Empty>
      <EmptyHeader>
        <EmptyMedia variant="icon">
        {Icon ? <Icon className={spinning ? "spin" : undefined} /> : <AvatarStack />}
        </EmptyMedia>
        <EmptyTitle>{title}</EmptyTitle>
        <EmptyDescription>{description}</EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <UiButton variant={variant === "outline" ? "outline" : "default"}>{variant === "primary" ? <Plus /> : null} {action}</UiButton>
      </EmptyContent>
    </Empty>
  )
}

function MetricCard({ label, value, detail }: { label: string; value: string; detail: string }) {
  return (
    <UiCard className="card metric-card">
      <span>{label}</span>
      <strong>{value}</strong>
      <p>{detail}</p>
    </UiCard>
  )
}

function CalendarBlock() {
  return <Calendar className="rounded-md border" defaultMonth={new Date(2026, 5, 1)} mode="single" selected={new Date(2026, 5, 12)} />
}

function AvatarStack() {
  return (
    <div className="avatar-stack" aria-hidden="true">
      <img src="https://github.com/shadcn.png" alt="" />
      <img src="https://github.com/maxleiter.png" alt="" />
      <img src="https://github.com/evilrabbit.png" alt="" />
    </div>
  )
}

function SkeletonBlock() {
  return (
    <div className="grid gap-3">
      <Skeleton className="h-3 w-1/2" />
      <Skeleton className="h-3 w-3/4" />
      <Skeleton className="h-3 w-2/3" />
      <div className="flex items-center gap-2 text-sm text-muted-foreground"><LoaderCircle className="spin" /> Loading primitives fit into any shell.</div>
    </div>
  )
}

function tabValue(item: string) {
  return item.toLowerCase().replace(/\s+/g, "-")
}

function DemoTabs({ items, value, onValueChange }: { items: string[]; value?: string; onValueChange?: (value: string) => void }) {
  return (
    <Tabs className="tabs" value={value} defaultValue={value ? undefined : tabValue(items[0])} onValueChange={onValueChange}>
      <TabsList>
        {items.map((item) => <TabsTrigger key={item} value={tabValue(item)}>{item}</TabsTrigger>)}
      </TabsList>
    </Tabs>
  )
}

function DemoPagination({ pages = 3, page = 1, onPageChange }: { pages?: number; page?: number; onPageChange?: (page: number) => void }) {
  const [internalPage, setInternalPage] = useState(page)
  const currentPage = onPageChange ? page : internalPage

  function selectPage(event: React.MouseEvent<HTMLAnchorElement>, nextPage: number) {
    event.preventDefault()
    const boundedPage = Math.max(1, Math.min(pages, nextPage))
    if (onPageChange) {
      onPageChange(boundedPage)
      return
    }
    setInternalPage(boundedPage)
  }

  return (
    <Pagination className="pagination compact">
      <PaginationContent>
        <PaginationItem>
          <PaginationPrevious
            className={currentPage === 1 ? "pointer-events-none opacity-50" : undefined}
            href="#"
            onClick={(event) => selectPage(event, currentPage - 1)}
          />
        </PaginationItem>
        {Array.from({ length: pages }, (_, index) => (
          <PaginationItem key={index}>
            <PaginationLink href="#" isActive={index + 1 === currentPage} onClick={(event) => selectPage(event, index + 1)}>{index + 1}</PaginationLink>
          </PaginationItem>
        ))}
        <PaginationItem>
          <PaginationNext
            className={currentPage === pages ? "pointer-events-none opacity-50" : undefined}
            href="#"
            onClick={(event) => selectPage(event, currentPage + 1)}
          />
        </PaginationItem>
      </PaginationContent>
    </Pagination>
  )
}

function ToggleRow({ title, description, checked = false }: { title: string; description: string; checked?: boolean }) {
  return (
    <Item variant="outline" size="sm">
      <ItemContent>
        <ItemTitle>{title}</ItemTitle>
        <ItemDescription>{description}</ItemDescription>
      </ItemContent>
      <ItemActions><Switch defaultChecked={checked} /></ItemActions>
    </Item>
  )
}

function renderFieldControl(children: ReactNode): ReactNode {
  if (!React.isValidElement(children)) {
    return children
  }

  if (children.type === "input") {
    const props = children.props as React.ComponentProps<"input">
    if (props.type === "checkbox" || props.type === "radio" || props.type === "range") {
      return children
    }
    return <Input {...props} />
  }

  if (children.type === "textarea") {
    return <Textarea {...(children.props as React.ComponentProps<"textarea">)} />
  }

  if (children.type === "select") {
    const { size: _size, ...props } = children.props as React.ComponentProps<"select">
    return <NativeSelect {...props} />
  }

  return children
}

function SettingsHeader({ title, description }: { title: string; description: string }) {
  return <div className="settings-header"><h1>{title}</h1><p>{description}</p></div>
}

function initials(user: AuthUser) {
  const parts = displayName(user).trim().split(/\s+/).filter(Boolean)
  if (parts.length >= 2) {
    return `${parts[0][0]}${parts[1][0]}`.toUpperCase()
  }
  return displayName(user).slice(0, 2).toUpperCase()
}

function displayName(user: AuthUser) {
  return user.display_name || user.username || "User"
}
