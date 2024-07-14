import re
import subprocess
import tempfile
from typing import Any
from mkdocs.config.defaults import MkDocsConfig


def on_config(config: MkDocsConfig) -> MkDocsConfig | None:
    mdx_configs = config.setdefault("mdx_configs", {})
    superfences = mdx_configs.setdefault("pymdownx.superfences", {})
    custom_fences = superfences.setdefault("custom_fences", [])
    custom_fences.append({
        "name": "freeze",
        "class": "freeze",
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
    default_center = "true"
    if "float" in inputs:
        options["float"] = inputs.pop("float")
        default_center = "false"

    if "width" in inputs:
        options["width"] = inputs.pop("width")
    if "language" in inputs:
        options["language"] = inputs.pop("language")

    options["center"] = inputs.pop("center", default_center) == "true"
    if options["center"]:
        options.pop("float", None)

    return "language" in options


# freeze generates svgs with a width and height parameter.
# We can use that to generate a viewBox attribute,
# allowing the svg to scale to the width of the container.
_widthRe = re.compile(r'width="(?P<width>[\d\.]+)"')
_heightRe = re.compile(r'height="(?P<height>[\d\.]+)"')


_terminalReplacements = [
    ('\\x1b', '\x1b'),
    ('{red}', '\x1b[0;31m'),
    ('{green}', '\x1b[0;32m'),
    ('{yellow}', '\x1b[0;33m'),
    ('{blue}', '\x1b[0;34m'),
    ('{mag}', '\x1b[0;35m'),
    ('{cyan}', '\x1b[0;36m'),
    ('{gray}', '\x1b[0;90m'),
    ('{reset}', '\x1b[0;0m'),
]


# run freeze --window --language=$language in a subprocess,
# writing to a temporary file.
def _formatter(
    source: str,
    language: str,
    css_class: str,
    options: dict[str, Any],
    *args, **kwargs,
):
    # Convenience:
    # If language is terminal, we actually want "ansi",
    # but we want to transform escapes in the source.
    if options["language"] == "terminal":
        options["language"] = "ansi"
        for pattern, replacement in _terminalReplacements:
            source = source.replace(pattern, replacement)

    with tempfile.TemporaryDirectory() as tmpdir:
        outfile = f"{tmpdir}/output.svg"
        try:
            args = [
                "freeze",
                "--language", options["language"],
                "--output", outfile,
                # same as -c full, but with no shadow.
                # auto-width calculation is broken with shadow.
                "--window", "--theme=charm",
                "--border.radius=8", "--border.width=1",
                "--border.color=#515151",
                "--padding=10,10,10,10",
                "--margin=10,10,10,10",
                "--background=#171717",
            ]
            subprocess.run(
                args, input=source.encode("utf-8"), check=True,
            )
        except subprocess.CalledProcessError as e:
            return f'<code>{e.output.decode("utf-8")}</code>'
        except Exception as e:
            return f'<code>{e}</code>'

        with open(outfile, "r") as f:
            svg = f.read()

    width, height = None, None
    if m := _widthRe.search(svg):
        width = m.group("width")
    if m := _heightRe.search(svg):
        height = m.group("height")
    if not width or not height:
        return '<code>Could not find width and height in svg</code>'

    # insert viewBox="0 0 width height" into the svg,
    # and drop the width and height attributes.
    svg = svg.replace(
        '<svg', f'<svg viewBox="0 0 {width} {height}"', 1,
    )
    svg = svg.replace(f' width="{width}"', "", 1)
    svg = svg.replace(f' height="{height}"', "", 1)

    if "width" in options:
        width = options["width"]
        height = "auto"
    else:
        width = f"{width}px"
        height = f"{height}px"

    style = f'width:{width};height:{height};max-width:100%;'
    if "float" in options:
        style += f'float:{options["float"]};'
    if options["center"]:
        style += 'margin:0 auto;'

    return f'<div style="{style}">{svg}</div>'
