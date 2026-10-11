#!/usr/bin/env python3
"""Write Screenpear.xcodeproj for the native iOS lane.

One iOS app target over build/swift-shared/*.swift and
build/ios-swiftui/swift/*.swift, with the lane's Info.plist and asset
catalog. Deterministic: object ids are md5 of their role, so re-running
produces the same file and Xcode does not see spurious changes.

    python3 build/ios-swiftui/xcode/gen.py [--team ABCDE12345]
"""
import argparse
import hashlib
import os
import plistlib

HERE = os.path.dirname(os.path.abspath(__file__))
LANE = os.path.dirname(HERE)                      # build/ios-swiftui
ROOT = os.path.dirname(os.path.dirname(LANE))     # repo root
PROJ = os.path.join(HERE, "Screenpear.xcodeproj")


def oid(name):
    return hashlib.md5(name.encode()).hexdigest()[:24].upper()


def rel(path):
    return os.path.relpath(path, HERE)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--team", default="")
    args = ap.parse_args()

    with open(os.path.join(LANE, "Info.plist"), "rb") as f:
        info = plistlib.load(f)
    bundle_id = info["CFBundleIdentifier"]
    min_ios = info.get("MinimumOSVersion", "17.0")
    version = info.get("CFBundleShortVersionString", "0.1.0")
    # The plist is shared with the swiftc lane, which names the binary in
    # lower case; Xcode must produce the same file or installd rejects the app.
    executable = info.get("CFBundleExecutable", "screenpear")

    sources = sorted(
        os.path.join(d, n)
        for d in (os.path.join(ROOT, "build", "swift-shared"), os.path.join(LANE, "swift"))
        for n in os.listdir(d) if n.endswith(".swift")
    )
    assets = os.path.join(LANE, "Assets.xcassets")
    plist = os.path.join(LANE, "Info.plist")

    objs = {}

    def add(key, body):
        objs[oid(key)] = body
        return oid(key)

    src_refs, build_files = [], []
    for s in sources:
        r = add("ref:" + s, f'{{isa = PBXFileReference; lastKnownFileType = sourcecode.swift; name = "{os.path.basename(s)}"; path = "{rel(s)}"; sourceTree = "<group>"; }}')
        b = add("build:" + s, f'{{isa = PBXBuildFile; fileRef = {r}; }}')
        src_refs.append(r)
        build_files.append(b)
    assets_ref = add("ref:assets", f'{{isa = PBXFileReference; lastKnownFileType = folder.assetcatalog; name = "Assets.xcassets"; path = "{rel(assets)}"; sourceTree = "<group>"; }}')
    assets_build = add("build:assets", f'{{isa = PBXBuildFile; fileRef = {assets_ref}; }}')
    plist_ref = add("ref:plist", f'{{isa = PBXFileReference; lastKnownFileType = text.plist.xml; name = "Info.plist"; path = "{rel(plist)}"; sourceTree = "<group>"; }}')
    product = add("ref:product", '{isa = PBXFileReference; explicitFileType = wrapper.application; includeInIndex = 0; path = "Screenpear.app"; sourceTree = BUILT_PRODUCTS_DIR; }')

    sources_group = add("group:sources", f'{{isa = PBXGroup; children = ({", ".join(src_refs)}); name = Sources; sourceTree = "<group>"; }}')
    products_group = add("group:products", f'{{isa = PBXGroup; children = ({product}); name = Products; sourceTree = "<group>"; }}')
    main_group = add("group:main", f'{{isa = PBXGroup; children = ({sources_group}, {assets_ref}, {plist_ref}, {products_group}); sourceTree = "<group>"; }}')

    sources_phase = add("phase:sources", f'{{isa = PBXSourcesBuildPhase; buildActionMask = 2147483647; files = ({", ".join(build_files)}); runOnlyForDeploymentPostprocessing = 0; }}')
    resources_phase = add("phase:resources", f'{{isa = PBXResourcesBuildPhase; buildActionMask = 2147483647; files = ({assets_build}); runOnlyForDeploymentPostprocessing = 0; }}')

    team = f'DEVELOPMENT_TEAM = "{args.team}";' if args.team else ""
    common = f'''
        ASSETCATALOG_COMPILER_APPICON_NAME = AppIcon;
        CODE_SIGN_STYLE = Automatic;
        {team}
        CURRENT_PROJECT_VERSION = 1;
        GENERATE_INFOPLIST_FILE = NO;
        INFOPLIST_FILE = "{rel(plist)}";
        IPHONEOS_DEPLOYMENT_TARGET = {min_ios};
        LD_RUNPATH_SEARCH_PATHS = ("$(inherited)", "@executable_path/Frameworks");
        MARKETING_VERSION = {version};
        PRODUCT_BUNDLE_IDENTIFIER = "{bundle_id}";
        PRODUCT_NAME = Screenpear;
        EXECUTABLE_NAME = "{executable}";
        SDKROOT = iphoneos;
        SWIFT_VERSION = 5.0;
        TARGETED_DEVICE_FAMILY = "1,2";
    '''
    t_debug = add("cfg:target:debug", f'{{isa = XCBuildConfiguration; buildSettings = {{{common} SWIFT_OPTIMIZATION_LEVEL = "-Onone"; DEBUG_INFORMATION_FORMAT = dwarf; }}; name = Debug; }}')
    t_release = add("cfg:target:release", f'{{isa = XCBuildConfiguration; buildSettings = {{{common} SWIFT_OPTIMIZATION_LEVEL = "-O"; }}; name = Release; }}')
    p_debug = add("cfg:project:debug", '{isa = XCBuildConfiguration; buildSettings = { ALWAYS_SEARCH_USER_PATHS = NO; CLANG_ENABLE_MODULES = YES; ENABLE_TESTABILITY = YES; SWIFT_ACTIVE_COMPILATION_CONDITIONS = DEBUG; }; name = Debug; }')
    p_release = add("cfg:project:release", '{isa = XCBuildConfiguration; buildSettings = { ALWAYS_SEARCH_USER_PATHS = NO; CLANG_ENABLE_MODULES = YES; ENABLE_NS_ASSERTIONS = NO; VALIDATE_PRODUCT = YES; }; name = Release; }')
    t_list = add("list:target", f'{{isa = XCConfigurationList; buildConfigurations = ({t_debug}, {t_release}); defaultConfigurationIsVisible = 0; defaultConfigurationName = Release; }}')
    p_list = add("list:project", f'{{isa = XCConfigurationList; buildConfigurations = ({p_debug}, {p_release}); defaultConfigurationIsVisible = 0; defaultConfigurationName = Release; }}')

    target = add("target", f'{{isa = PBXNativeTarget; buildConfigurationList = {t_list}; buildPhases = ({sources_phase}, {resources_phase}); buildRules = (); dependencies = (); name = Screenpear; productName = Screenpear; productReference = {product}; productType = "com.apple.product-type.application"; }}')
    project = add("project", f'{{isa = PBXProject; attributes = {{ LastUpgradeCheck = 1600; TargetAttributes = {{ {target} = {{ CreatedOnToolsVersion = 16.0; }}; }}; }}; buildConfigurationList = {p_list}; compatibilityVersion = "Xcode 14.0"; developmentRegion = en; hasScannedForEncodings = 0; knownRegions = (en, Base); mainGroup = {main_group}; productRefGroup = {products_group}; projectDirPath = ""; projectRoot = ""; targets = ({target}); }}')

    body = "\n".join(f"\t\t{k} = {v};" for k, v in objs.items())
    pbx = f"""// !$*UTF8*$!
{{
\tarchiveVersion = 1;
\tclasses = {{
\t}};
\tobjectVersion = 56;
\tobjects = {{
{body}
\t}};
\trootObject = {project};
}}
"""
    os.makedirs(PROJ, exist_ok=True)
    with open(os.path.join(PROJ, "project.pbxproj"), "w") as f:
        f.write(pbx)
    print(f"wrote {rel(PROJ)} ({len(sources)} sources, bundle {bundle_id}, team {args.team or 'unset: pick in Signing & Capabilities'})")


if __name__ == "__main__":
    main()
