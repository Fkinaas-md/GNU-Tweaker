package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// --- Определение дистрибутива ---

type DistroInfo struct {
	ID       string
	Name     string
	Codename string
}

func detectDistro() DistroInfo {
	info := DistroInfo{ID: "unknown", Name: "Unknown Linux"}
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return info
	}
	defer file.Close()

	var idLike string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		val := strings.Trim(parts[1], `"'`)

		switch key {
		case "ID":
			info.ID = strings.ToLower(val)
		case "ID_LIKE":
			idLike = strings.ToLower(val)
		case "PRETTY_NAME":
			info.Name = val
		case "VERSION_CODENAME":
			info.Codename = strings.ToLower(val)
		}
	}

	if info.ID != "arch" && strings.Contains(idLike, "arch") {
		info.ID = "arch"
	} else if info.ID != "debian" && strings.Contains(idLike, "debian") {
		info.ID = "debian"
	}

	return info
}

// --- Настройки, состояние и логи ---

type state struct {
	Selected map[string]bool `json:"selected"`
}

func statePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "gnu-tweaker", "state.json")
}

func logPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "gnu-tweaker-last-run.log")
	}
	return filepath.Join(home, ".local", "state", "gnu-tweaker", "last-run.log")
}

func loadState() state {
	s := state{Selected: make(map[string]bool)}
	data, err := os.ReadFile(statePath())
	if err == nil {
		json.Unmarshal(data, &s)
	}
	if s.Selected == nil {
		s.Selected = make(map[string]bool)
	}
	return s
}

func saveState(allTweaks []tweak) {
	s := state{Selected: make(map[string]bool)}
	for _, t := range allTweaks {
		if t.category == "tool" {
			continue
		}
		s.Selected[t.id] = t.selected
	}

	path := statePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err == nil {
		if data, err := json.MarshalIndent(s, "", "  "); err == nil {
			os.WriteFile(path, data, 0644)
		}
	}
}

// --- Структуры данных ---

type tweak struct {
	id          string
	category    string // "packages", "system", "kernel", "gaming", "dev", "wm", "tool"
	title       string
	description string
	distros     []string // "all", "arch", "debian"
	selected    bool
	applied     bool
	checkCmd    []string
	commands    [][]string
}

type categoryTab struct {
	id    string
	label string
}

type Preset struct {
	ID          string
	Name        string
	Description string
	TweakIDs    []string
}

type model struct {
	distro       DistroInfo
	tabs         []categoryTab
	activeTab    int
	allTweaks    []tweak
	visibleIdx   []int
	cursor       int
	inspectMode  bool
	executed     bool
	presets      []Preset
	presetMenu   bool
	presetCursor int
	statusMsg    string
}

func checkTweakApplied(cmdArgs []string) bool {
	if len(cmdArgs) == 0 {
		return false
	}
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	return cmd.Run() == nil
}

func currentBinaryPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "./gnu-tweaker"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved
	}
	return exe
}

func getPresets(distro DistroInfo) []Preset {
	if distro.ID == "arch" {
		return []Preset{
			{
				ID:          "dev",
				Name:        "Разработчик (Developer)",
				Description: "Стек разработки: Git, Neovim, Tmux, Zsh, Docker, шрифты JetBrains Mono, makepkg, ZRAM, SSD trim, sysctl BBR.",
				TweakIDs: []string{
					"arch-update", "arch-pacman-conf", "arch-makepkg", "arch-dev-stack",
					"arch-docker", "sys-ssd-trim", "sys-zram-arch", "sys-journald", "kern-sysctl",
				},
			},
			{
				ID:          "gamer",
				Name:        "Геймер (Gaming & Proton)",
				Description: "Игровая станция: Steam, GameMode, MangoHud, Wine, Proton sysctl (vm.max_map_count, BBR), ZRAM, SSD trim, автоочистка кэша.",
				TweakIDs: []string{
					"arch-update", "arch-gaming-tools", "kern-sysctl",
					"sys-zram-arch", "sys-ssd-trim", "arch-paccache",
				},
			},
			{
				ID:          "wm-wayland",
				Name:        "Оконный менеджер Wayland (Hyprland / Sway)",
				Description: "Современный тайлинг: Hyprland, Waybar, Wofi, Kitty, Grim, Slurp, Dunst, Polkit, PipeWire, FontAwesome, ZRAM.",
				TweakIDs: []string{
					"arch-wm-hyprland", "arch-pipewire", "arch-dev-stack",
					"sys-ssd-trim", "sys-zram-arch",
				},
			},
			{
				ID:          "wm-i3",
				Name:        "Оконный менеджер X11 (i3-wm)",
				Description: "Классический тайлинг X11: i3-wm, Polybar, Picom, Rofi, Feh, Dunst, Flameshot, Kitty, xclip, PipeWire, ZRAM.",
				TweakIDs: []string{
					"arch-wm-i3", "arch-pipewire", "arch-dev-stack",
					"sys-ssd-trim", "sys-zram-arch",
				},
			},
		}
	}

	// Debian / Default
	return []Preset{
		{
			ID:          "dev",
			Name:        "Разработчик (Developer)",
			Description: "Стек разработки: Git, Neovim, Tmux, Zsh, Docker, JetBrains Mono, Python, headers, Nala, ZRAM, SSD trim, sysctl BBR.",
			TweakIDs: []string{
				"deb-update", "deb-nala", "deb-dev-headers", "deb-dev-stack",
				"deb-docker", "sys-ssd-trim", "sys-zram-deb", "sys-journald", "kern-sysctl",
			},
		},
		{
			ID:          "gamer",
			Name:        "Геймер (Gaming & Proton)",
			Description: "Игровая станция: Steam, GameMode, MangoHud, Wine, Proton sysctl (vm.max_map_count, BBR), non-free, ZRAM, SSD trim, очистка apt.",
			TweakIDs: []string{
				"deb-update", "deb-nonfree", "deb-gaming-tools", "kern-sysctl",
				"sys-zram-deb", "sys-ssd-trim", "deb-clean",
			},
		},
		{
			ID:          "wm-wayland",
			Name:        "Оконный менеджер Wayland (Sway)",
			Description: "Стек Sway Wayland: Sway, Waybar, Wofi, Swaybg, Swaylock, Kitty, Grim, Slurp, Mako, Policykit, PipeWire, FontAwesome, ZRAM.",
			TweakIDs: []string{
				"deb-wm-sway", "deb-pipewire", "deb-dev-stack",
				"sys-ssd-trim", "sys-zram-deb",
			},
		},
		{
			ID:          "wm-i3",
			Name:        "Оконный менеджер X11 (i3-wm)",
			Description: "Классический тайлинг X11: i3, Polybar, Picom, Rofi, Feh, Dunst, Flameshot, Kitty, xclip, PipeWire, ZRAM.",
			TweakIDs: []string{
				"deb-wm-i3", "deb-pipewire", "deb-dev-stack",
				"sys-ssd-trim", "sys-zram-deb",
			},
		},
	}
}

func buildTweaks(distro DistroInfo, saved state) []tweak {
	pick := func(id string, def bool) bool {
		if v, ok := saved.Selected[id]; ok {
			return v
		}
		return def
	}

	binPath := currentBinaryPath()
	installTarget := "/usr/bin/gnu-tweaker"
	homeDir, _ := os.UserHomeDir()
	nvidiaRcPath := filepath.Join(homeDir, ".nvidia-settings-rc")

	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("LOGNAME")
	}
	if username == "" {
		username = "1000"
	}

	// Скрипт добавления nvidia-drm.modeset=1 в GRUB или systemd-boot
	nvidiaDrmScript := `
if [ -f /etc/default/grub ]; then
	if ! grep -q "nvidia-drm.modeset=1" /etc/default/grub; then
		sed -i 's/\(^GRUB_CMDLINE_LINUX_DEFAULT="[^"]*\)"/\1 nvidia-drm.modeset=1"/' /etc/default/grub
	fi
	if command -v update-grub >/dev/null 2>&1; then
		update-grub
	elif command -v grub-mkconfig >/dev/null 2>&1; then
		grub-mkconfig -o /boot/grub/grub.cfg
	fi
elif [ -d /boot/loader/entries ]; then
	for f in /boot/loader/entries/*.conf; do
		[ -f "$f" ] && ! grep -q "nvidia-drm.modeset=1" "$f" && sed -i '/^options / s/$/ nvidia-drm.modeset=1/' "$f"
	done
else
	echo "Не найден GRUB или systemd-boot. Добавьте nvidia-drm.modeset=1 вручную."
	exit 1
fi`

	nvidiaXorgConf := `Section "Device"
    Identifier     "Nvidia Card"
    Driver         "nvidia"
    VendorName     "NVIDIA Corporation"
    Option         "NoLogo" "true"
    Option         "TripleBuffer" "true"
    Option         "Coolbits" "28"
EndSection

Section "Screen"
    Identifier     "Screen0"
    Device         "Nvidia Card"
    DefaultDepth    24
    Option         "metamodes" "nvidia-auto-select +0+0 {ForceFullCompositionPipeline=On}"
    Option         "AllowIndirectGLXProtocol" "off"
    SubSection     "Display"
        Depth       24
    EndSubSection
EndSection

Section "Extensions"
    Option         "Composite" "Enable"
EndSection`

	nvidiaSettingsRc := `# Configuration file for nvidia-settings - generated by gnu-tweaker
RcFileLocale = C
DisplayStatusBar = Yes
SliderTextEntries = Yes
IncludeDisplayNameInConfigFile = No
UpdateRulesOnProfileNameChange = Yes
Timer = Memory_Used_(GPU_0),Yes,3000
Timer = Thermal_Monitor_(GPU_0),Yes,1000
Timer = PowerMizer_Monitor_(GPU_0),Yes,1000

0/SyncToVBlank=1
0/LogAniso=0
0/FSAA=0
0/TextureClamping=1
0/FXAA=0
0/AllowFlipping=1
0/FSAAAppControlled=1
0/LogAnisoAppControlled=1
0/OpenGLImageSettings=0
0/FSAAAppEnhanced=0
0/ShowGraphicsVisualIndicator=0
[DPY:DVI-D-0]/Dithering=0
[DPY:DVI-D-0]/DitheringMode=0
[DPY:DVI-D-0]/DitheringDepth=0
[DPY:DVI-D-0]/ColorSpace=0
[DPY:DVI-D-0]/ColorRange=0
[DPY:DVI-D-0]/SynchronousPaletteUpdates=0
[DPY:HDMI-0]/Dithering=0
[DPY:HDMI-0]/DitheringMode=0
[DPY:HDMI-0]/DitheringDepth=0
[DPY:HDMI-0]/ColorSpace=0
[DPY:HDMI-0]/ColorRange=0
[DPY:HDMI-0]/SynchronousPaletteUpdates=0
[DPY:DP-0]/RedBrightness=0.000000
[DPY:DP-0]/GreenBrightness=0.000000
[DPY:DP-0]/BlueBrightness=0.000000
[DPY:DP-0]/RedContrast=0.000000
[DPY:DP-0]/GreenContrast=0.000000
[DPY:DP-0]/BlueContrast=0.000000
[DPY:DP-0]/RedGamma=1.000000
[DPY:DP-0]/GreenGamma=1.000000
[DPY:DP-0]/BlueGamma=1.000000
[DPY:DP-0]/Dithering=0
[DPY:DP-0]/DitheringMode=0
[DPY:DP-0]/DitheringDepth=0
[DPY:DP-0]/DigitalVibrance=0
[DPY:DP-0]/ColorSpace=0
[DPY:DP-0]/ColorRange=0
[DPY:DP-0]/SynchronousPaletteUpdates=0
[DPY:DP-1]/Dithering=0
[DPY:DP-1]/DitheringMode=0
[DPY:DP-1]/DitheringDepth=0
[DPY:DP-1]/ColorSpace=0
[DPY:DP-1]/ColorRange=0
[DPY:DP-1]/SynchronousPaletteUpdates=0
[GPU:0]/GPUPowerMizerMode=1`

	rawTweaks := []tweak{
		// ==================== ПАКЕТЫ (ARCH) ====================
		{
			id:          "arch-update",
			category:    "packages",
			title:       "Обновление системы (pacman -Syu)",
			description: "Полное обновление всех установленных пакетов и баз данных Arch Linux.",
			distros:     []string{"arch"},
			selected:    pick("arch-update", false),
			commands:    [][]string{{"sudo", "pacman", "-Syu", "--noconfirm"}},
		},
		{
			id:          "arch-pacman-conf",
			category:    "packages",
			title:       "Твики pacman.conf (Color, ILoveCandy, ParallelDownloads)",
			description: "Включает цветной вывод, анимацию Пакмана и параллельную загрузку в 5 потоков.",
			distros:     []string{"arch"},
			selected:    pick("arch-pacman-conf", false),
			checkCmd:    []string{"grep", "-q", "^ILoveCandy", "/etc/pacman.conf"},
			commands: [][]string{
				{"sudo", "sed", "-i", "s/^#Color/Color/", "/etc/pacman.conf"},
				{"sudo", "sed", "-i", "s/^#ParallelDownloads.*/ParallelDownloads = 5/", "/etc/pacman.conf"},
				{"sudo", "bash", "-c", "grep -q '^ILoveCandy' /etc/pacman.conf || sed -i '/^Color/a ILoveCandy' /etc/pacman.conf"},
			},
		},
		{
			id:          "arch-makepkg",
			category:    "packages",
			title:       "Многопоточная сборка в makepkg.conf",
			description: "Настраивает MAKEFLAGS=\"-j$(nproc)\" и многопоточное сжатие zstd для быстрой сборки из AUR.",
			distros:     []string{"arch"},
			selected:    pick("arch-makepkg", false),
			checkCmd:    []string{"grep", "-q", `^MAKEFLAGS="-j`, "/etc/makepkg.conf"},
			commands: [][]string{
				{"sudo", "sed", "-i", `s/^#MAKEFLAGS=.*/MAKEFLAGS="-j$(nproc)"/`, "/etc/makepkg.conf"},
				{"sudo", "sed", "-i", `s/COMPRESSZST=(zstd -c -z -q -)/COMPRESSZST=(zstd -c -T0 -z -q -)/`, "/etc/makepkg.conf"},
			},
		},
		{
			id:          "arch-mirrors",
			category:    "packages",
			title:       "Оптимизация зеркал (reflector)",
			description: "Устанавливает reflector и выбирает 5 самых быстрых HTTPS-зеркал.",
			distros:     []string{"arch"},
			selected:    pick("arch-mirrors", false),
			commands: [][]string{
				{"sudo", "pacman", "-S", "reflector", "--noconfirm", "--needed"},
				{"sudo", "reflector", "--latest", "5", "--protocol", "https", "--sort", "rate", "--save", "/etc/pacman.d/mirrorlist"},
			},
		},
		{
			id:          "arch-yay",
			category:    "packages",
			title:       "Установка AUR-помощника yay-bin",
			description: "Клонирует и собирает yay-bin из AUR, затем удаляет временные файлы из /tmp.",
			distros:     []string{"arch"},
			selected:    pick("arch-yay", false),
			checkCmd:    []string{"which", "yay"},
			commands: [][]string{
				{"sudo", "pacman", "-S", "--needed", "git", "base-devel", "--noconfirm"},
				{"bash", "-c", "rm -rf /tmp/yay-bin && git clone https://aur.archlinux.org/yay-bin.git /tmp/yay-bin && cd /tmp/yay-bin && makepkg -si --noconfirm && rm -rf /tmp/yay-bin"},
			},
		},
		{
			id:          "arch-paccache",
			category:    "packages",
			title:       "Автоочистка кэша (paccache.timer)",
			description: "Устанавливает pacman-contrib и включает еженедельную очистку старых версий пакетов.",
			distros:     []string{"arch"},
			selected:    pick("arch-paccache", false),
			checkCmd:    []string{"systemctl", "is-enabled", "paccache.timer"},
			commands: [][]string{
				{"sudo", "pacman", "-S", "--needed", "pacman-contrib", "--noconfirm"},
				{"sudo", "systemctl", "enable", "--now", "paccache.timer"},
			},
		},
		{
			id:          "arch-orphans",
			category:    "packages",
			title:       "Удаление пакетов-сирот (pacman -Qdtq)",
			description: "Удаляет неиспользуемые зависимости вместе с их конфигами.",
			distros:     []string{"arch"},
			selected:    pick("arch-orphans", false),
			commands:    [][]string{{"bash", "-c", "sudo pacman -Qdtq | xargs -r sudo pacman -Rns --noconfirm"}},
		},

		// ==================== ПАКЕТЫ (DEBIAN 13) ====================
		{
			id:          "deb-update",
			category:    "packages",
			title:       "Обновление системы (apt full-upgrade)",
			description: "Обновляет списки пакетов и выполняет полное обновление Debian 13 (Trixie).",
			distros:     []string{"debian"},
			selected:    pick("deb-update", false),
			commands: [][]string{
				{"sudo", "apt", "update"},
				{"sudo", "apt", "full-upgrade", "-y"},
			},
		},
		{
			id:          "deb-nonfree",
			category:    "packages",
			title:       "Подключить contrib и non-free-firmware",
			description: "Добавляет компоненты contrib, non-free и non-free-firmware в репозитории Debian (DEB822 и legacy).",
			distros:     []string{"debian"},
			selected:    pick("deb-nonfree", false),
			checkCmd:    []string{"grep", "-rq", "non-free-firmware", "/etc/apt/sources.list", "/etc/apt/sources.list.d/"},
			commands: [][]string{
				{"sudo", "bash", "-c", `if [ -f /etc/apt/sources.list.d/debian.sources ]; then sed -i 's/^Components: main$/Components: main contrib non-free non-free-firmware/' /etc/apt/sources.list.d/debian.sources; fi`},
				{"sudo", "bash", "-c", `if [ -f /etc/apt/sources.list ]; then sed -i 's/ main$/ main contrib non-free non-free-firmware/' /etc/apt/sources.list; fi`},
				{"sudo", "apt", "update"},
			},
		},
		{
			id:          "deb-nala",
			category:    "packages",
			title:       "Установка Nala (улучшенный фронтенд для APT)",
			description: "Устанавливает nala — быструю и читаемую обертку над apt с параллельной загрузкой.",
			distros:     []string{"debian"},
			selected:    pick("deb-nala", false),
			checkCmd:    []string{"which", "nala"},
			commands: [][]string{
				{"sudo", "apt", "update"},
				{"sudo", "apt", "install", "-y", "nala"},
			},
		},
		{
			id:          "deb-dev-headers",
			category:    "packages",
			title:       "Установка build-essential и linux-headers",
			description: "Устанавливает компиляторы и заголовки ядра для сборки модулей DKMS (NVIDIA, VirtualBox и др.).",
			distros:     []string{"debian"},
			selected:    pick("deb-dev-headers", false),
			checkCmd:    []string{"dpkg", "-s", "build-essential"},
			commands: [][]string{
				{"sudo", "apt", "install", "-y", "build-essential", "linux-headers-amd64", "curl", "git"},
			},
		},
		{
			id:          "deb-clean",
			category:    "packages",
			title:       "Очистка кэша и удаление сирот (apt autoremove --purge)",
			description: "Удаляет неиспользуемые пакеты и очищает локальный кэш загруженных .deb файлов.",
			distros:     []string{"debian"},
			selected:    pick("deb-clean", false),
			commands: [][]string{
				{"sudo", "apt", "autoremove", "--purge", "-y"},
				{"sudo", "apt", "clean"},
			},
		},

		// ==================== СИСТЕМА ====================
		{
			id:          "deb-nvidia-full",
			category:    "system",
			title:       "Драйверы NVIDIA (nvidia-driver-full + 32-bit libs)",
			description: "Включает i386, подключает non-free, ставит headers, nvidia-driver-full и nvidia-driver-libs:i386.",
			distros:     []string{"debian"},
			selected:    pick("deb-nvidia-full", false),
			checkCmd:    []string{"dpkg", "-s", "nvidia-driver-full", "nvidia-driver-libs:i386"},
			commands: [][]string{
				{"sudo", "dpkg", "--add-architecture", "i386"},
				{"sudo", "bash", "-c", `if [ -f /etc/apt/sources.list.d/debian.sources ]; then sed -i 's/^Components: main$/Components: main contrib non-free non-free-firmware/' /etc/apt/sources.list.d/debian.sources; fi`},
				{"sudo", "bash", "-c", `if [ -f /etc/apt/sources.list ]; then sed -i 's/ main$/ main contrib non-free non-free-firmware/' /etc/apt/sources.list; fi`},
				{"sudo", "apt", "update"},
				{"sudo", "apt", "install", "-y", "linux-headers-amd64", "nvidia-driver-full", "nvidia-driver-libs:i386"},
			},
		},
		{
			id:          "arch-nvidia-full",
			category:    "system",
			title:       "Драйверы NVIDIA (nvidia-dkms + lib32-nvidia-utils)",
			description: "Включает репозиторий multilib, ставит linux-headers, nvidia-dkms, nvidia-utils и 32-битные библиотеки.",
			distros:     []string{"arch"},
			selected:    pick("arch-nvidia-full", false),
			checkCmd:    []string{"pacman", "-Qi", "nvidia-dkms", "lib32-nvidia-utils"},
			commands: [][]string{
				{"sudo", "sed", "-i", `/^#\[multilib\]/,+1 s/^#//`, "/etc/pacman.conf"},
				{"sudo", "pacman", "-Sy", "--needed", "linux-headers", "nvidia-dkms", "nvidia-utils", "lib32-nvidia-utils", "nvidia-settings", "--noconfirm"},
			},
		},
		{
			id:          "sys-nvidia-drm",
			category:    "system",
			title:       "Включить NVIDIA DRM Modeset (nvidia-drm.modeset=1)",
			description: "Добавляет nvidia-drm.modeset=1 в параметры ядра (GRUB или systemd-boot) для Wayland и без тиринга.",
			distros:     []string{"all"},
			selected:    pick("sys-nvidia-drm", false),
			checkCmd:    []string{"bash", "-c", `grep -q "nvidia-drm.modeset=1" /etc/default/grub /boot/loader/entries/*.conf 2>/dev/null || grep -qx "Y" /sys/module/nvidia_drm/parameters/modeset 2>/dev/null`},
			commands: [][]string{
				{"sudo", "bash", "-c", strings.TrimSpace(nvidiaDrmScript)},
			},
		},
		{
			id:          "sys-nvidia-x11-conf",
			category:    "system",
			title:       "Конфиг X11 NVIDIA (ForceFullCompositionPipeline, TripleBuffer, Coolbits)",
			description: "Записывает /etc/X11/xorg.conf.d/20-nvidia.conf (убирает тиринг в X11, включает Coolbits 28 и NoLogo).",
			distros:     []string{"all"},
			selected:    pick("sys-nvidia-x11-conf", false),
			checkCmd:    []string{"test", "-f", "/etc/X11/xorg.conf.d/20-nvidia.conf"},
			commands: [][]string{
				{"sudo", "mkdir", "-p", "/etc/X11/xorg.conf.d"},
				{"sudo", "bash", "-c", fmt.Sprintf("cat << 'EOF' > /etc/X11/xorg.conf.d/20-nvidia.conf\n%s\nEOF", nvidiaXorgConf)},
			},
		},
		{
			id:          "sys-nvidia-settings-rc",
			category:    "system",
			title:       "Профиль nvidia-settings (~/.nvidia-settings-rc, Prefer Max Performance)",
			description: "Записывает ~/.nvidia-settings-rc (PowerMizerMode=1, отключение дизеринга) и применяет настройки.",
			distros:     []string{"all"},
			selected:    pick("sys-nvidia-settings-rc", false),
			checkCmd:    []string{"grep", "-q", "GPUPowerMizerMode=1", nvidiaRcPath},
			commands: [][]string{
				{"bash", "-c", fmt.Sprintf("cat << 'EOF' > %s\n%s\nEOF", nvidiaRcPath, nvidiaSettingsRc)},
				{"bash", "-c", "command -v nvidia-settings >/dev/null 2>&1 && nvidia-settings --load-config-only || true"},
			},
		},
		{
			id:          "sys-ssd-trim",
			category:    "system",
			title:       "Оптимизация SSD (fstrim.timer)",
			description: "Включает еженедельный TRIM для продления срока службы и скорости SSD.",
			distros:     []string{"all"},
			selected:    pick("sys-ssd-trim", false),
			checkCmd:    []string{"systemctl", "is-enabled", "fstrim.timer"},
			commands: [][]string{
				{"sudo", "systemctl", "enable", "--now", "fstrim.timer"},
			},
		},
		{
			id:          "sys-journald",
			category:    "system",
			title:       "Ограничение размера логов journald (до 100M)",
			description: "Создает /etc/systemd/journald.conf.d/size.conf с лимитом SystemMaxUse=100M.",
			distros:     []string{"all"},
			selected:    pick("sys-journald", false),
			checkCmd:    []string{"test", "-f", "/etc/systemd/journald.conf.d/size.conf"},
			commands: [][]string{
				{"sudo", "mkdir", "-p", "/etc/systemd/journald.conf.d"},
				{"sudo", "bash", "-c", `printf "[Journal]\nSystemMaxUse=100M\n" > /etc/systemd/journald.conf.d/size.conf`},
				{"sudo", "systemctl", "restart", "systemd-journald"},
			},
		},
		{
			id:          "sys-zram-arch",
			category:    "system",
			title:       "Настройка ZRAM (zram-generator + zstd)",
			description: "Создает сжатый swap-раздел в оперативной памяти (половина объема ОЗУ, алгоритм zstd).",
			distros:     []string{"arch"},
			selected:    pick("sys-zram-arch", false),
			checkCmd:    []string{"test", "-f", "/etc/systemd/zram-generator.conf"},
			commands: [][]string{
				{"sudo", "pacman", "-S", "--needed", "zram-generator", "--noconfirm"},
				{"sudo", "bash", "-c", `printf "[zram0]\nzram-size = ram / 2\ncompression-algorithm = zstd\n" > /etc/systemd/zram-generator.conf`},
				{"sudo", "systemctl", "daemon-reload"},
				{"sudo", "systemctl", "start", "systemd-zram-setup@zram0.service"},
			},
		},
		{
			id:          "sys-zram-deb",
			category:    "system",
			title:       "Настройка ZRAM (systemd-zram-generator + zstd)",
			description: "Устанавливает systemd-zram-generator и настраивает сжатие zstd в оперативной памяти.",
			distros:     []string{"debian"},
			selected:    pick("sys-zram-deb", false),
			checkCmd:    []string{"test", "-f", "/etc/systemd/zram-generator.conf"},
			commands: [][]string{
				{"sudo", "apt", "install", "-y", "systemd-zram-generator"},
				{"sudo", "bash", "-c", `printf "[zram0]\nzram-size = ram / 2\ncompression-algorithm = zstd\n" > /etc/systemd/zram-generator.conf`},
				{"sudo", "systemctl", "daemon-reload"},
				{"sudo", "systemctl", "start", "systemd-zram-setup@zram0.service"},
			},
		},
		{
			id:          "deb-grub-fast",
			category:    "system",
			title:       "Ускорение загрузки GRUB (таймаут 1 сек)",
			description: "Меняет GRUB_TIMEOUT на 1 секунду в /etc/default/grub и обновляет конфигурацию.",
			distros:     []string{"debian"},
			selected:    pick("deb-grub-fast", false),
			checkCmd:    []string{"grep", "-q", "^GRUB_TIMEOUT=1$", "/etc/default/grub"},
			commands: [][]string{
				{"sudo", "sed", "-i", "s/^GRUB_TIMEOUT=.*/GRUB_TIMEOUT=1/", "/etc/default/grub"},
				{"sudo", "update-grub"},
			},
		},

		// ==================== СЕТЬ И ЯДРО ====================
		{
			id:          "kern-sysctl",
			category:    "kernel",
			title:       "Оптимизация sysctl (BBR, max_map_count для игр, swappiness)",
			description: "Включает TCP BBR, повышает vm.max_map_count (2147483642 для Proton/Wine) и выставляет swappiness.",
			distros:     []string{"all"},
			selected:    pick("kern-sysctl", false),
			checkCmd:    []string{"test", "-f", "/etc/sysctl.d/99-gnu-tweaker.conf"},
			commands: [][]string{
				{"sudo", "bash", "-c", `cat << 'EOF' > /etc/sysctl.d/99-gnu-tweaker.conf
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
vm.max_map_count = 2147483642
vm.swappiness = 100
EOF`},
				{"sudo", "sysctl", "--system"},
			},
		},
		{
			id:          "net-ufw-arch",
			category:    "kernel",
			title:       "Базовая настройка фаервола (UFW)",
			description: "Устанавливает ufw, блокирует входящие соединения, разрешает исходящие и включает службу.",
			distros:     []string{"arch"},
			selected:    pick("net-ufw-arch", false),
			checkCmd:    []string{"systemctl", "is-enabled", "ufw.service"},
			commands: [][]string{
				{"sudo", "pacman", "-S", "--needed", "ufw", "--noconfirm"},
				{"sudo", "ufw", "default", "deny", "incoming"},
				{"sudo", "ufw", "default", "allow", "outgoing"},
				{"sudo", "ufw", "--force", "enable"},
				{"sudo", "systemctl", "enable", "--now", "ufw.service"},
			},
		},
		{
			id:          "net-ufw-deb",
			category:    "kernel",
			title:       "Базовая настройка фаервола (UFW)",
			description: "Устанавливает ufw, запрещает входящие по умолчанию и включает защиту.",
			distros:     []string{"debian"},
			selected:    pick("net-ufw-deb", false),
			checkCmd:    []string{"systemctl", "is-enabled", "ufw.service"},
			commands: [][]string{
				{"sudo", "apt", "install", "-y", "ufw"},
				{"sudo", "ufw", "default", "deny", "incoming"},
				{"sudo", "ufw", "default", "allow", "outgoing"},
				{"sudo", "ufw", "--force", "enable"},
			},
		},

		// ==================== ГЕЙМИНГ ====================
		{
			id:          "arch-gaming-tools",
			category:    "gaming",
			title:       "Игровой стек (Steam, GameMode, MangoHud, Wine)",
			description: "Включает multilib, устанавливает Steam, gamemode, mangohud, goverlay и wine для игр.",
			distros:     []string{"arch"},
			selected:    pick("arch-gaming-tools", false),
			checkCmd:    []string{"which", "steam"},
			commands: [][]string{
				{"sudo", "sed", "-i", `/^#\[multilib\]/,+1 s/^#//`, "/etc/pacman.conf"},
				{"sudo", "pacman", "-Sy", "--needed", "steam", "gamemode", "lib32-gamemode", "mangohud", "lib32-mangohud", "wine", "goverlay", "--noconfirm"},
			},
		},
		{
			id:          "deb-gaming-tools",
			category:    "gaming",
			title:       "Игровой стек (Steam, GameMode, MangoHud, Wine)",
			description: "Включает архитектуру i386, репозитории non-free, ставит steam-installer, gamemode, mangohud, goverlay и wine.",
			distros:     []string{"debian"},
			selected:    pick("deb-gaming-tools", false),
			checkCmd:    []string{"which", "gamemoded"},
			commands: [][]string{
				{"sudo", "dpkg", "--add-architecture", "i386"},
				{"sudo", "bash", "-c", `if [ -f /etc/apt/sources.list.d/debian.sources ]; then sed -i 's/^Components: main$/Components: main contrib non-free non-free-firmware/' /etc/apt/sources.list.d/debian.sources; fi`},
				{"sudo", "bash", "-c", `if [ -f /etc/apt/sources.list ]; then sed -i 's/ main$/ main contrib non-free non-free-firmware/' /etc/apt/sources.list; fi`},
				{"sudo", "apt", "update"},
				{"sudo", "apt", "install", "-y", "steam-installer", "gamemode", "mangohud", "goverlay", "wine", "wine32", "wine64"},
			},
		},

		// ==================== РАЗРАБОТКА ====================
		{
			id:          "arch-dev-stack",
			category:    "dev",
			title:       "Стек разработчика (Git, Neovim, Tmux, Zsh, JetBrains Mono)",
			description: "Устанавливает: git, base-devel, curl, wget, jq, ripgrep, htop, tmux, neovim, zsh и шрифты JetBrains Mono.",
			distros:     []string{"arch"},
			selected:    pick("arch-dev-stack", false),
			checkCmd:    []string{"which", "nvim"},
			commands: [][]string{
				{"sudo", "pacman", "-S", "--needed", "git", "base-devel", "curl", "wget", "jq", "ripgrep", "htop", "tmux", "neovim", "zsh", "ttf-jetbrains-mono-nerd", "--noconfirm"},
			},
		},
		{
			id:          "deb-dev-stack",
			category:    "dev",
			title:       "Стек разработчика (Git, Neovim, Tmux, Zsh, JetBrains Mono, Python)",
			description: "Устанавливает: git, build-essential, curl, wget, jq, ripgrep, htop, tmux, neovim, zsh, python3-venv и шрифты JetBrains Mono.",
			distros:     []string{"debian"},
			selected:    pick("deb-dev-stack", false),
			checkCmd:    []string{"which", "nvim"},
			commands: [][]string{
				{"sudo", "apt", "update"},
				{"sudo", "apt", "install", "-y", "git", "build-essential", "curl", "wget", "jq", "ripgrep", "htop", "tmux", "neovim", "zsh", "python3-venv", "python3-pip", "fonts-jetbrains-mono"},
			},
		},
		{
			id:          "arch-docker",
			category:    "dev",
			title:       "Контейнеризация (Docker & Docker Compose)",
			description: "Устанавливает Docker, compose, запускает службу и добавляет текущего пользователя в группу docker.",
			distros:     []string{"arch"},
			selected:    pick("arch-docker", false),
			checkCmd:    []string{"systemctl", "is-enabled", "docker.service"},
			commands: [][]string{
				{"sudo", "pacman", "-S", "--needed", "docker", "docker-compose", "--noconfirm"},
				{"sudo", "systemctl", "enable", "--now", "docker.service"},
				{"sudo", "usermod", "-aG", "docker", username},
			},
		},
		{
			id:          "deb-docker",
			category:    "dev",
			title:       "Контейнеризация (Docker & Docker Compose)",
			description: "Устанавливает docker.io, docker-compose-plugin, включает службу docker и добавляет пользователя в группу docker.",
			distros:     []string{"debian"},
			selected:    pick("deb-docker", false),
			checkCmd:    []string{"systemctl", "is-enabled", "docker.service"},
			commands: [][]string{
				{"sudo", "apt", "install", "-y", "docker.io", "docker-compose-plugin"},
				{"sudo", "systemctl", "enable", "--now", "docker"},
				{"sudo", "usermod", "-aG", "docker", username},
			},
		},

		// ==================== ОКОННЫЕ МЕНЕДЖЕРЫ ====================
		{
			id:          "arch-wm-hyprland",
			category:    "wm",
			title:       "Стек Hyprland (современный Wayland-тайлинг + зависимости)",
			description: "Устанавливает Hyprland, Waybar, Wofi, Kitty, Hyprpaper, Hyprlock, Hypridle, Grim, Slurp, wl-clipboard, Dunst, Polkit и порталы.",
			distros:     []string{"arch"},
			selected:    pick("arch-wm-hyprland", false),
			checkCmd:    []string{"which", "hyprland"},
			commands: [][]string{
				{"sudo", "pacman", "-S", "--needed", "hyprland", "waybar", "wofi", "kitty", "hyprpaper", "hyprlock", "hypridle", "grim", "slurp", "wl-clipboard", "dunst", "polkit-gnome", "xdg-desktop-portal-hyprland", "xdg-desktop-portal-gtk", "ttf-font-awesome", "--noconfirm"},
			},
		},
		{
			id:          "arch-wm-sway",
			category:    "wm",
			title:       "Стек Sway (i3-совместимый Wayland + зависимости)",
			description: "Устанавливает Sway, Waybar, Wofi, Swaybg, Swaylock, Swayidle, Kitty, Grim, Slurp, wl-clipboard, Mako, Polkit и xdg-desktop-portal-wlr.",
			distros:     []string{"arch"},
			selected:    pick("arch-wm-sway", false),
			checkCmd:    []string{"which", "sway"},
			commands: [][]string{
				{"sudo", "pacman", "-S", "--needed", "sway", "waybar", "wofi", "swaybg", "swaylock", "swayidle", "foot", "kitty", "grim", "slurp", "wl-clipboard", "mako", "polkit-gnome", "xdg-desktop-portal-wlr", "xdg-desktop-portal-gtk", "ttf-font-awesome", "--noconfirm"},
			},
		},
		{
			id:          "deb-wm-sway",
			category:    "wm",
			title:       "Стек Sway (современный Wayland-тайлинг + зависимости)",
			description: "Устанавливает Sway, Waybar, Wofi, Swaybg, Swaylock, Swayidle, Kitty, Grim, Slurp, wl-clipboard, Mako, Policykit и порталы Wayland.",
			distros:     []string{"debian"},
			selected:    pick("deb-wm-sway", false),
			checkCmd:    []string{"which", "sway"},
			commands: [][]string{
				{"sudo", "apt", "install", "-y", "sway", "waybar", "wofi", "swaybg", "swaylock", "swayidle", "kitty", "grim", "slurp", "wl-clipboard", "mako-notifier", "policykit-1-gnome", "xdg-desktop-portal-wlr", "xdg-desktop-portal-gtk", "fonts-font-awesome"},
			},
		},
		{
			id:          "arch-wm-i3",
			category:    "wm",
			title:       "Стек i3-wm (классический тайлинг X11 + зависимости)",
			description: "Устанавливает i3-wm, i3status, i3lock, picom (композитор), polybar, rofi, feh, dunst, flameshot, kitty, xclip, xorg.",
			distros:     []string{"arch"},
			selected:    pick("arch-wm-i3", false),
			checkCmd:    []string{"which", "i3"},
			commands: [][]string{
				{"sudo", "pacman", "-S", "--needed", "i3-wm", "i3status", "i3lock", "picom", "polybar", "rofi", "feh", "dunst", "flameshot", "kitty", "xclip", "xorg-xinit", "xorg-server", "--noconfirm"},
			},
		},
		{
			id:          "deb-wm-i3",
			category:    "wm",
			title:       "Стек i3-wm (классический тайлинг X11 + зависимости)",
			description: "Устанавливает i3, polybar, picom (композитор), rofi, feh, dunst, flameshot, kitty, xclip, xorg.",
			distros:     []string{"debian"},
			selected:    pick("deb-wm-i3", false),
			checkCmd:    []string{"which", "i3"},
			commands: [][]string{
				{"sudo", "apt", "install", "-y", "i3", "i3status", "i3lock", "polybar", "picom", "rofi", "feh", "dunst", "flameshot", "kitty", "xclip", "xorg"},
			},
		},
		{
			id:          "arch-pipewire",
			category:    "wm",
			title:       "Звуковой сервер PipeWire & WirePlumber (для WM)",
			description: "Устанавливает и включает аудиостек PipeWire с низкой задержкой для тайлинговых окружений.",
			distros:     []string{"arch"},
			selected:    pick("arch-pipewire", false),
			checkCmd:    []string{"which", "pipewire"},
			commands: [][]string{
				{"sudo", "pacman", "-S", "--needed", "pipewire", "pipewire-pulse", "pipewire-alsa", "pipewire-jack", "wireplumber", "--noconfirm"},
				{"systemctl", "--user", "enable", "--now", "pipewire", "pipewire-pulse", "wireplumber"},
			},
		},
		{
			id:          "deb-pipewire",
			category:    "wm",
			title:       "Звуковой сервер PipeWire & WirePlumber (для WM)",
			description: "Устанавливает и включает аудиостек PipeWire с низкой задержкой для тайлинговых окружений.",
			distros:     []string{"debian"},
			selected:    pick("deb-pipewire", false),
			checkCmd:    []string{"which", "pipewire"},
			commands: [][]string{
				{"sudo", "apt", "install", "-y", "pipewire-audio", "wireplumber", "pipewire-pulse"},
				{"systemctl", "--user", "enable", "--now", "pipewire", "pipewire-pulse", "wireplumber"},
			},
		},

		// ==================== УТИЛИТА ====================
		{
			id:          "tool-install-usr-bin",
			category:    "tool",
			title:       "Установить утилиту в /usr/bin/gnu-tweaker",
			description: fmt.Sprintf("Копирует текущий бинарник (%s) в %s для запуска из любой директории.", binPath, installTarget),
			distros:     []string{"all"},
			selected:    false,
			checkCmd:    []string{"test", "-f", installTarget},
			commands: [][]string{
				{"sudo", "install", "-Dm755", binPath, installTarget},
			},
		},
		{
			id:          "tool-uninstall-usr-bin",
			category:    "tool",
			title:       "Удалить утилиту из /usr/bin/gnu-tweaker",
			description: fmt.Sprintf("Удаляет установленный исполняемый файл %s из системы.", installTarget),
			distros:     []string{"all"},
			selected:    false,
			commands: [][]string{
				{"sudo", "rm", "-f", installTarget},
			},
		},
	}

	var filtered []tweak
	for _, t := range rawTweaks {
		supported := false
		for _, d := range t.distros {
			if d == "all" || d == distro.ID {
				supported = true
				break
			}
		}
		if !supported {
			continue
		}
		t.applied = checkTweakApplied(t.checkCmd)
		filtered = append(filtered, t)
	}

	return filtered
}

func initialModel() model {
	distro := detectDistro()
	saved := loadState()
	tweaks := buildTweaks(distro, saved)
	presets := getPresets(distro)

	tabs := []categoryTab{
		{id: "all", label: "Все"},
		{id: "packages", label: "Пакеты"},
		{id: "system", label: "Система"},
		{id: "kernel", label: "Сеть и Ядро"},
		{id: "gaming", label: "Гейминг"},
		{id: "dev", label: "Разработка"},
		{id: "wm", label: "Оконные WM"},
		{id: "tool", label: "Утилита"},
	}

	m := model{
		distro:    distro,
		tabs:      tabs,
		activeTab: 0,
		allTweaks: tweaks,
		presets:   presets,
	}

	m.refreshVisible()
	return m
}

func (m *model) applyPreset(p Preset) {
	targetMap := make(map[string]bool)
	for _, id := range p.TweakIDs {
		targetMap[id] = true
	}
	count := 0
	for i := range m.allTweaks {
		if m.allTweaks[i].category == "tool" {
			continue
		}
		if targetMap[m.allTweaks[i].id] {
			m.allTweaks[i].selected = true
			count++
		} else {
			m.allTweaks[i].selected = false
		}
	}
	m.statusMsg = fmt.Sprintf("Применен пресет: \"%s\" (отмечено %d твиков)", p.Name, count)
}

func (m *model) refreshVisible() {
	m.visibleIdx = nil
	activeCat := m.tabs[m.activeTab].id
	for i, t := range m.allTweaks {
		if activeCat == "all" {
			if t.category != "tool" {
				m.visibleIdx = append(m.visibleIdx, i)
			}
		} else if t.category == activeCat {
			m.visibleIdx = append(m.visibleIdx, i)
		}
	}
	if m.cursor >= len(m.visibleIdx) {
		m.cursor = 0
	}
}

func (m *model) setAllVisible(val bool) {
	for _, realIdx := range m.visibleIdx {
		m.allTweaks[realIdx].selected = val
	}
	if val {
		m.statusMsg = "Отмечены все видимые пункты"
	} else {
		m.statusMsg = "Сняты отметки со всех видимых пунктов"
	}
}

// --- Логика Bubble Tea ---

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.presetMenu {
			switch msg.String() {
			case "ctrl+c", "q", "esc", "p", "P":
				m.presetMenu = false
				return m, nil
			case "up", "k":
				if m.presetCursor > 0 {
					m.presetCursor--
				}
			case "down", "j":
				if m.presetCursor < len(m.presets)-1 {
					m.presetCursor++
				}
			case "enter", " ":
				if len(m.presets) > 0 {
					m.applyPreset(m.presets[m.presetCursor])
					m.presetMenu = false
				}
				return m, nil
			case "1":
				if len(m.presets) > 0 {
					m.applyPreset(m.presets[0])
					m.presetMenu = false
				}
				return m, nil
			case "2":
				if len(m.presets) > 1 {
					m.applyPreset(m.presets[1])
					m.presetMenu = false
				}
				return m, nil
			case "3":
				if len(m.presets) > 2 {
					m.applyPreset(m.presets[2])
					m.presetMenu = false
				}
				return m, nil
			case "4":
				if len(m.presets) > 3 {
					m.applyPreset(m.presets[3])
					m.presetMenu = false
				}
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "p", "P":
			m.presetMenu = true
			return m, nil
		case "1":
			if len(m.presets) > 0 {
				m.applyPreset(m.presets[0])
			}
		case "2":
			if len(m.presets) > 1 {
				m.applyPreset(m.presets[1])
			}
		case "3":
			if len(m.presets) > 2 {
				m.applyPreset(m.presets[2])
			}
		case "4":
			if len(m.presets) > 3 {
				m.applyPreset(m.presets[3])
			}
		case "tab", "right", "l":
			m.activeTab = (m.activeTab + 1) % len(m.tabs)
			m.cursor = 0
			m.refreshVisible()
		case "shift+tab", "left", "h":
			m.activeTab = (m.activeTab - 1 + len(m.tabs)) % len(m.tabs)
			m.cursor = 0
			m.refreshVisible()
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.visibleIdx)-1 {
				m.cursor++
			}
		case " ", "enter":
			if len(m.visibleIdx) > 0 {
				realIdx := m.visibleIdx[m.cursor]
				m.allTweaks[realIdx].selected = !m.allTweaks[realIdx].selected
			}
		case "a":
			m.setAllVisible(true)
		case "n", "0":
			m.setAllVisible(false)
		case "i", "?":
			m.inspectMode = !m.inspectMode
		case "x":
			m.executed = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) viewPresetMenu() string {
	var b strings.Builder
	b.WriteString("============================= ВЫБОР ПРЕСЕТА =============================\n")
	b.WriteString("Быстрая настройка системы под ваш сценарий использования.\n")
	b.WriteString("Выберите профиль, чтобы автоматически отметить необходимый набор твиков:\n\n")

	for i, p := range m.presets {
		cursor := "  "
		if m.presetCursor == i {
			cursor = "> "
		}
		b.WriteString(fmt.Sprintf("%s[%d] %s\n", cursor, i+1, p.Name))
		b.WriteString(fmt.Sprintf("      %s\n\n", p.Description))
	}

	b.WriteString(strings.Repeat("-", 75) + "\n")
	b.WriteString("[↑/↓] Выбор стрелками  [Enter] Применить  [1-4] Быстрый выбор  [Esc / P] Отмена\n")
	return b.String()
}

func (m model) View() string {
	if m.executed {
		return "Выход из интерфейса настройки. Начинаем применение твиков...\n"
	}

	if m.presetMenu {
		return m.viewPresetMenu()
	}

	var b strings.Builder

	distroLabel := m.distro.Name
	if m.distro.Codename != "" {
		distroLabel += fmt.Sprintf(" (%s)", m.distro.Codename)
	}
	b.WriteString(fmt.Sprintf("=== GNU-TWEAKER | ОС: %s ===\n\n", distroLabel))

	if m.distro.ID != "arch" && m.distro.ID != "debian" {
		b.WriteString("[!] Внимание: дистрибутив не распознан как Arch или Debian. Доступны только общие твики.\n\n")
	}

	// Отрисовка вкладок
	for i, tab := range m.tabs {
		if i == m.activeTab {
			b.WriteString(fmt.Sprintf("[ %s ]  ", strings.ToUpper(tab.label)))
		} else {
			b.WriteString(fmt.Sprintf("  %s    ", tab.label))
		}
	}
	b.WriteString("\n" + strings.Repeat("-", 78) + "\n\n")

	// Статусное сообщение (если применен пресет)
	if m.statusMsg != "" {
		b.WriteString(fmt.Sprintf("Статус: %s\n\n", m.statusMsg))
	}

	// Список твиков текущей вкладки
	if len(m.visibleIdx) == 0 {
		b.WriteString("  Нет доступных пунктов в этой категории.\n")
	} else {
		for i, realIdx := range m.visibleIdx {
			t := m.allTweaks[realIdx]
			cursor := " "
			if m.cursor == i {
				cursor = ">"
			}
			checked := " "
			if t.selected {
				checked = "X"
			}
			status := ""
			if t.applied {
				status = " [активно]"
			}
			b.WriteString(fmt.Sprintf("%s [%s] %s%s\n", cursor, checked, t.title, status))
		}
	}

	// Блок описания и превью команд выбранного твика
	b.WriteString("\n" + strings.Repeat("-", 78) + "\n")
	if len(m.visibleIdx) > 0 {
		current := m.allTweaks[m.visibleIdx[m.cursor]]
		b.WriteString(fmt.Sprintf("Описание: %s\n", current.description))

		if m.inspectMode {
			b.WriteString("\nКоманды для выполнения:\n")
			for _, cmd := range current.commands {
				b.WriteString(fmt.Sprintf("  $ %s\n", strings.Join(cmd, " ")))
			}
		}
	}

	b.WriteString("\n[↑/↓] Навигация  [Tab/←/→] Вкладки  [Пробел] Выбор  [1-4 / P] Пресеты  [A/N] Все/Снять  [I] Команды  [X] Запуск  [Q] Выход\n")

	return b.String()
}

// --- Выполнение и логирование ---

func setupLogger() (io.Writer, io.Writer, *os.File, string) {
	path := logPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return os.Stdout, os.Stderr, nil, ""
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return os.Stdout, os.Stderr, nil, ""
	}
	fmt.Fprintf(f, "=== GNU-TWEAKER LOG (%s) ===\n\n", time.Now().Format("2006-01-02 15:04:05"))
	return io.MultiWriter(os.Stdout, f), io.MultiWriter(os.Stderr, f), f, path
}

func main() {
	if os.Geteuid() == 0 {
		fmt.Println("[Ошибка] Не запускайте утилиту от имени root (через sudo).")
		fmt.Println("Запустите от обычного пользователя — sudo будет запрошен автоматически для нужных команд.")
		os.Exit(1)
	}

	p := tea.NewProgram(initialModel())
	finalModelRaw, err := p.Run()
	if err != nil {
		fmt.Printf("Ошибка запуска интерфейса: %v\n", err)
		os.Exit(1)
	}

	m := finalModelRaw.(model)
	saveState(m.allTweaks)

	if !m.executed {
		fmt.Println("\nВыход без изменений.")
		return
	}

	outWriter, errWriter, logFile, savedLogPath := setupLogger()
	if logFile != nil {
		defer logFile.Close()
	}

	fmt.Fprintln(outWriter, "\n[+] Запрос прав администратора для применения выбранных твиков...")
	sudoCheck := exec.Command("sudo", "-v")
	sudoCheck.Stdout = os.Stdout
	sudoCheck.Stderr = os.Stderr
	sudoCheck.Stdin = os.Stdin
	if err := sudoCheck.Run(); err != nil {
		fmt.Fprintln(errWriter, "[Ошибка] Не удалось получить права sudo.")
		os.Exit(1)
	}

	for _, t := range m.allTweaks {
		if !t.selected {
			continue
		}

		fmt.Fprintf(outWriter, "\n--> Выполняется: %s\n", t.title)
		for _, cmdArgs := range t.commands {
			if len(cmdArgs) == 0 {
				continue
			}

			fmt.Fprintf(outWriter, "Запуск: %s\n", strings.Join(cmdArgs, " "))
			cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
			cmd.Stdout = outWriter
			cmd.Stderr = errWriter
			cmd.Stdin = os.Stdin

			if err := cmd.Run(); err != nil {
				fmt.Fprintf(errWriter, "[Ошибка] Не удалось выполнить: %v\n", err)
				break
			}
		}
	}

	fmt.Fprintln(outWriter, "\n[Все готово!] Выбранные настройки применены.")
	if savedLogPath != "" {
		fmt.Printf("Лог выполнения сохранен в: %s\n", savedLogPath)
	}
}
