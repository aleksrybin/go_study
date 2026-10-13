package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SystemStats хранит данные о памяти в MMегабайтах..
type SystemStats struct {
	Total     int
	Available int
}

type LoadManager struct {
	TargetCPU    float64
	TargetRAMPct float64
	Duration     time.Duration

	// Слайс для хранения блоков памяти, чтобы GC их не удалил
	memoryBlocks [][]byte
	mu           sync.Mutex
}

// Функция получения статистики памяти через PowerShell
func getSystemStats() (SystemStats, error) {
	// Получаем TotalVisibleMemorySize и FreePhysicalMemory
	psCommand := "Get-CimInstance Win32_OperatingSystem | Select-Object TotalVisibleMemorySize, FreePhysicalMemory"
	out, err := exec.Command("powershell", "-Command", psCommand).Output()
	if err != nil {
		return SystemStats{}, err
	}

	lines := strings.Split(string(out), "\n")
	var cleanLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Игнорируем пустые строки, заголовки и разделители таблицы
		if trimmed != "" && !strings.Contains(trimmed, "---") && !strings.Contains(trimmed, "MemorySize") {
			cleanLines = append(cleanLines, trimmed)
		}
	}

	if len(cleanLines) < 1 {
		return SystemStats{}, fmt.Errorf("данные памяти не найдены в выводе PowerShell")
	}

	// Парсим первую строку с данными (разделена пробелами)
	fields := strings.Fields(cleanLines[0])
	if len(fields) < 2 {
		return SystemStats{}, fmt.Errorf("недостаточно полей в выводе памяти")
	}

	totalKB, _ := strconv.Atoi(fields[0])
	freeKB, _ := strconv.Atoi(fields[1])

	return SystemStats{
		Total:     totalKB / 1024,
		Available: freeKB / 1024,
	}, nil
}

// Генератор нагрузки на CPU
func (m *LoadManager) cpuWorker() {
	for {
		workDuration := time.Duration(float64(100*time.Millisecond) * m.TargetCPU)
		sleepDuration := 100*time.Millisecond - workDuration

		start := time.Now()
		for time.Since(start) < workDuration {
			_ = 1.2345 * 6.7890
		}
		time.Sleep(sleepDuration)
	}
}

// Корректировка использования RAM
func (m *LoadManager) adjustMemory() {
	m.mu.Lock()
	defer m.mu.Unlock()

	stats, err := getSystemStats()
	if err != nil {
		fmt.Printf("[!] Ошибка обновления статистики: %v\n", err)
		return
	}

	currentUsedTotal := stats.Total - stats.Available
	targetTotalUsed := int(float64(stats.Total) * m.TargetRAMPct)
	diff := targetTotalUsed - currentUsedTotal

	fmt.Printf("[Stats] System Total: %dMB | Current Used: %dMB | Target: %dMB | Diff: %dMB\n",
		stats.Total, currentUsedTotal, targetTotalUsed, diff)

	if diff > 0 {
		fmt.Printf("Adding %d MB to reach target...\n", diff)
		block := make([]byte, diff*1024*1024)
		for i := 0; i < len(block); i += 4096 {
			block[i] = 1
		}
		m.memoryBlocks = append(m.memoryBlocks, block)
	} else if diff < -128 {
		fmt.Printf("Reducing load by %d MB...\n", -diff)
		// Освобождаем блоки, пока не приблизимся к цели
		for len(m.memoryBlocks) > 0 && (stats.Total-stats.Available) > targetTotalUsed {
			m.memoryBlocks = m.memoryBlocks[:len(m.memoryBlocks)-1]
			// В реальности память освободится после вызова GC
		}
		runtime.GC()
	}
}

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Использование: loadgen.exe <CPU%> <RAM%> <Seconds>")
		fmt.Println("Пример: loadgen.exe 50 30 60")
		return
	}

	// Парсинг аргументов
	cpuVal, _ := strconv.ParseFloat(os.Args[1], 64)
	ramVal, _ := strconv.ParseFloat(os.Args[2], 64)
	durationSec, _ := strconv.Atoi(os.Args[3])

	// Приведение процентов к десятичному виду (например, 50 -> 0.5)
	if cpuVal > 1.0 {
		cpuVal /= 100
	}
	if ramVal > 1.0 {
		ramVal /= 100
	}

	fmt.Println("--- Параметры нагрузки ---")
	fmt.Printf("Целевой CPU: %.2f%%\n", cpuVal*100)
	fmt.Printf("Целевой RAM: %.2f%%\n", ramVal*100)
	fmt.Printf("Время работы: %d сек\n", durationSec)
	fmt.Println("--------------------------")

	mgr := &LoadManager{
		TargetCPU:    cpuVal,
		TargetRAMPct: ramVal,
		Duration:     time.Duration(durationSec) * time.Second,
	}

	// Запуск CPU воркеров на все доступные ядра
	numCPU := runtime.NumCPU()
	for i := 0; i < numCPU; i++ {
		go mgr.cpuWorker()
	}

	// Первичная настройка памяти
	mgr.adjustMemory()

	stopChan := time.After(mgr.Duration)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			fmt.Println("\n[!] Время истекло. Освобождение ресурсов...")
			mgr.mu.Lock()
			mgr.memoryBlocks = nil
			runtime.GC()
			mgr.mu.Unlock()
			fmt.Println("Программа завершена.")
			return
		case <-ticker.C:
			mgr.adjustMemory()
		}
	}
}
