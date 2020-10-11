#! /bin/bash

_process_ubuntu ()
{
    echo "Post install script for Ubuntu"
    /bin/systemctl daemon-reload
    /bin/systemctl restart openconfigd.service
    return 0
}

_main ()
{
    _process_ubuntu
}

_main
