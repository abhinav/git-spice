import re
import subprocess
from typing import Any
from mkdocs.config.defaults import MkDocsConfig


def on_config(config: MkDocsConfig) -> MkDocsConfig | None:
    mdx_configs = config.setdefault("mdx_configs", {})
    superfences = mdx_configs.setdefault("pymdownx.superfences", {})
    custom_fences = superfences.setdefault("custom_fences", [])
    custom_fences.append({
        "name": "pikchr",
        "class": "pikchr",
        "validator": _validator,
        "format": _formatter
    })


def _validator(
    language: str,
    inputs: dict[str, str],
    options: dict[str, Any],
    attrs: dict[str, Any],  # noqa: ARG001
    *args, **kwargs,
) -> bool:
    options["width"] = inputs.pop("width", "100%")

    default_center = "true"
    if "float" in inputs:
        options["float"] = inputs.pop("float")
        default_center = "false"

    options["center"] = inputs.pop("center", default_center) == "true"
    if options["center"]:
        options.pop("float", None)

    return True


_viewboxRe = re.compile(
    r'viewBox="\d+ +\d+ +(?P<w>[\d\.]+) +(?P<h>[\d\.]+)"',
)


# Run 'pikchr -' with the source code as input
# and return the SVG output as a string.
def _formatter(
    source: str,
    language: str,
    css_class: str,
    options: dict[str, Any],
    *args, **kwargs,
):
    try:
        svg = subprocess.run(
            ["pikchr", "--dark-mode", "--svg-only", "-"],
            input=source.encode("utf-8"),
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=True
        ).stdout.decode("utf-8")

        width = options["width"]
        height = "auto"
        match = _viewboxRe.search(svg)
        if match:
            w, h = map(float, match.groups())
            width = f"{w}px"
            height = f"{h}px"

        style = f'width:{width};height:{height};max-width:100%;'
        if "float" in options:
            style += f'float:{options["float"]};'
        if options["center"]:
            style += 'margin:0 auto;'

        return f'<div style="{style}">{svg}</div>'
    except subprocess.CalledProcessError as e:
        return f'<code>{e.output.decode("utf-8")}</code>'
    except Exception as e:
        return f'<code>{e}</code>'
