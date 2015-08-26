Feature: Prompt for parent branch when unknown

  As a developer running a command on a branch without a parent branch
  I should see a prompt asking for the information
  So the command can work as I expect


  Scenario: prompting for parent branch when running git kill
    Given I have a feature branch named "feature" with no parent
    And I am on the "feature" branch
    When I run `git kill` and press ENTER
    Then I end up on the "main" branch
    And the existing branches are
      | REPOSITORY | BRANCHES |
      | local      | main     |