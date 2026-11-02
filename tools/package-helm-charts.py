#!/usr/bin/env python3
"""Stage and package versioned Logic Operator Helm charts for a release."""

from pathlib import Path
import re
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
CHARTS = ROOT / "charts"
OUTPUT = ROOT / "dist" / "helm"
KUSTOMIZE = ROOT / "bin" / "kustomize"


def documents(manifest: str) -> list[str]:
    return [doc.strip() for doc in re.split(r"(?m)^---\s*$", manifest) if doc.strip()]


def kind(document: str) -> str | None:
    match = re.search(r"(?m)^kind:\s*(\S+)\s*$", document)
    return match.group(1) if match else None


def build_kustomize(path: str) -> str:
    return subprocess.run(
        [str(KUSTOMIZE), "build", path],
        cwd=ROOT,
        check=True,
        text=True,
        capture_output=True,
    ).stdout


def generate_operator_chart(chart: Path) -> str:
    if not KUSTOMIZE.exists():
        raise SystemExit("bin/kustomize is missing; run `make kustomize` first")

    crds = [doc for doc in documents(build_kustomize("config/crd")) if kind(doc) == "CustomResourceDefinition"]
    if not crds:
        raise SystemExit("Kustomize produced no CRDs")
    crd_bundle = "\n---\n".join(crds) + "\n"

    resources = []
    for doc in documents(build_kustomize("config/default")):
        if kind(doc) in {"Namespace", "CustomResourceDefinition"}:
            continue
        doc = doc.replace("logic-operator-system", "{{ .Release.Namespace }}")
        doc = re.sub(
            r"(?m)^([ ]*)image:[ \t]*quay\.io/kubesmarts/logic-operator:[^ \t\r\n]+[ \t]*$",
            lambda match: (
                match.group(1) + 'image: {{ include "logic-operator.image" . }}\n'
                + match.group(1) + 'imagePullPolicy: {{ .Values.image.pullPolicy }}'
            ),
            doc,
        )
        resources.append(doc)

    if not resources:
        raise SystemExit("Kustomize produced no operator resources")
    resource_template = chart / "templates" / "resources.yaml"
    resource_template.parent.mkdir(parents=True, exist_ok=True)
    resource_template.write_text("\n---\n".join(resources) + "\n")
    return crd_bundle


def set_chart_versions(chart: Path, version: str) -> None:
    metadata = chart / "Chart.yaml"
    text = metadata.read_text()
    text = re.sub(
        r"(?m)^(\s*version:\s*).+$",
        lambda match: match.group(1) + version,
        text,
    )
    text = re.sub(
        r'(?m)^(\s*appVersion:\s*).+$',
        lambda match: match.group(1) + f'"{version}"',
        text,
    )
    metadata.write_text(text)


def main() -> None:
    if len(sys.argv) != 2 or not sys.argv[1]:
        raise SystemExit("usage: package-helm-charts.py VERSION")
    version = sys.argv[1]
    if version.startswith("v"):
        raise SystemExit("Chart versions must not include the Git tag's leading 'v'")

    if OUTPUT.exists():
        shutil.rmtree(OUTPUT)
    staging = OUTPUT / "staging"
    staging.mkdir(parents=True)
    for name in ("logic-operator", "logic-operator-stack"):
        shutil.copytree(CHARTS / name, staging / name)
        set_chart_versions(staging / name, version)
    crd_bundle = generate_operator_chart(staging / "logic-operator")

    subprocess.run(
        ["helm", "dependency", "update", "--skip-refresh", str(staging / "logic-operator-stack")],
        check=True,
    )
    for name in ("logic-operator", "logic-operator-stack"):
        subprocess.run(["helm", "lint", str(staging / name)], check=True)
    for name in ("logic-operator", "logic-operator-stack"):
        subprocess.run(
            ["helm", "package", str(staging / name), "--destination", str(OUTPUT)],
            check=True,
        )
    (OUTPUT / "crds.yaml").write_text(crd_bundle)
    shutil.rmtree(staging)


if __name__ == "__main__":
    main()
