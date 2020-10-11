#! /bin/bash

_process_ubuntu ()
{
    echo "Post uninstall script for Ubuntu"
    /bin/systemctl daemon-reload
    return 0
}

_main ()
{
    _process_ubuntu
}

_main
