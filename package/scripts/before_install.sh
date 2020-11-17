#! /bin/bash

_process_ubuntu ()
{
    echo "Pre install script for Ubuntu"
    /bin/systemctl stop openconfigd.service
    return 0
}

_main ()
{
    _process_ubuntu
}

_main
