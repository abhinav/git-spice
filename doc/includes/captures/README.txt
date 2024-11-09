This directory contains captured output from git-spice interactions.
To generate one:

1. Start capturing:

        script foo.txt

2. Use git-spice like you normally would.
   Type 'exit' to stop capturing.

        gs branch submit
        exit

3. Trim the resulting output in a text editor,
   removing unnecessary lines and commands.

4. Rename the file to something descriptive and place it in this directory.

5. Include these in the documentation under a 'freeze' block
   with language 'ansi':

        ```freeze language="ansi"
        --8<-- "captures/foo.txt"
        ```
