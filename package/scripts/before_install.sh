#! /bin/bash

_process_ubuntu ()
{
    echo "Pre install script for Ubuntu"
    /usr/bin/supervisorctl stop openconfigd
    return 0
}

_main ()
{
    _process_ubuntu
}

_main
